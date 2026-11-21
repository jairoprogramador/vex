package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"

	"github.com/jairoprogramador/vex/internal/domain/project/aggregates"
	proPor "github.com/jairoprogramador/vex/internal/domain/project/ports"
	proVos "github.com/jairoprogramador/vex/internal/domain/project/vos"
	"github.com/jairoprogramador/vex/internal/infrastructure/portalclient"
)

// ImagePuller deja disponible una imagen de registro antes de que haga falta.
type ImagePuller interface {
	Pull(ctx context.Context, image string) error
}

// PortalLogin es la sesión de vex en este equipo.
type PortalLogin interface {
	// EnsureToken no hace nada si ya hay sesión; si no, la inicia (abre el navegador en el portal).
	EnsureToken(ctx context.Context) error
	// ForgetToken descarta la sesión guardada cuando el portal la rechaza.
	ForgetToken() error
}

// ProjectPortal es lo que hace falta del portal para que conozca el proyecto.
type ProjectPortal interface {
	CreateOrGetProject(ctx context.Context, req portalclient.CreateOrGetProjectRequest) (portalclient.CreateOrGetProjectResponse, error)
	SyncPipeline(ctx context.Context, pipelineID string) error
}

// CloudPlan es con qué nube va a correr un intento: la conexión activa, el bootstrap del pipeline con el que se
// creó y el adaptador de su contrato. El valor cero es «sin portal»: las credenciales las pone el usuario.
type CloudPlan struct {
	Connection CloudConnectionInfo
	Bootstrap  PipelineBootstrap
	Adapter    CloudAdapter
}

// UsesPortal dice si el plan trae una conexión del portal.
func (p CloudPlan) UsesPortal() bool { return p.Adapter != nil }

// connection es la conexión para la sesión; nil si no hay plan.
func (p CloudPlan) connection() *CloudConnectionInfo {
	if !p.UsesPortal() {
		return nil
	}
	return &p.Connection
}

// CloudExecutionContext identifica la conexión y su destino con una huella corta ("azure/v1:3f9a1c0d2b7e4a65").
// Cambia si cambia la conexión (cada reconexión es otra) o el destino que define el adaptador; así el motor
// nunca reutiliza un paso hecho contra otra suscripción. No lleva secretos ni identificadores completos. Vacío
// sin portal: el motor no recibe el campo.
func CloudExecutionContext(plan CloudPlan) string {
	if !plan.UsesPortal() || plan.Connection.ConnectionID == "" || plan.Connection.Outputs == nil {
		return ""
	}
	key := plan.Adapter.Workload().ExecutionContextKey(plan.Connection.Outputs.Params)
	sum := sha256.Sum256([]byte(plan.Connection.ConnectionID + "\x00" + key))
	return string(plan.Connection.Outputs.ContractID) + ":" + hex.EncodeToString(sum[:8])
}

// CloudPreparer hace lo que hay que hacer antes de un intento cuando las credenciales salen del portal, en dos
// fases: Begin, antes de clonar (el portal puede corregir de qué pipeline se trata), y Ready, con el pipeline
// ya clonado. Nunca crea un contenedor: si algo falta, la ejecución termina aquí diciendo qué ejecutar.
type CloudPreparer struct {
	login      PortalLogin
	portal     ProjectPortal
	projects   proPor.ProjectRepository
	conns      CloudConnections
	bootstraps BootstrapReader
	adapters   CloudAdapters
	puller     ImagePuller
	notify     Notifier
	// catalog es el catálogo firmado contra el que se comparan las plantillas; nil si no se inyectó.
	catalog TemplateCatalog
	// approve pregunta antes de adoptar un cambio de pipeline o de imagen que proponga el portal; nil = nadie
	// puede aceptarlo.
	approve ChangeApprover
}

// WithChangeApprover indica quién confirma los cambios de pipeline o de imagen que propone el portal.
func (p *CloudPreparer) WithChangeApprover(approve ChangeApprover) *CloudPreparer {
	p.approve = approve
	return p
}

// WithTemplateCatalog hace que cada plantilla se compare con el catálogo firmado antes de usar su bootstrap.
func (p *CloudPreparer) WithTemplateCatalog(catalog TemplateCatalog) *CloudPreparer {
	p.catalog = catalog
	return p
}

// templateOrigin dice si el catálogo firmado respalda el bootstrap de este pipeline; falla si debería y no lo hace.
func (p *CloudPreparer) templateOrigin(project *aggregates.Project, pipeline Source, bootstrap PipelineBootstrap) (TemplateOrigin, error) {
	return CheckTemplateOrigin(p.catalog, project.Pipeline().URL(), pipeline.Commit, bootstrap)
}

func NewCloudPreparer(
	login PortalLogin, portal ProjectPortal, projects proPor.ProjectRepository, conns CloudConnections,
	bootstraps BootstrapReader, adapters CloudAdapters, puller ImagePuller, notify Notifier,
) *CloudPreparer {
	return &CloudPreparer{
		login: login, portal: portal, projects: projects, conns: conns,
		bootstraps: bootstraps, adapters: adapters, puller: puller, notify: notify,
	}
}

// Preparation es una preparación en curso, entre Begin y Ready. Hay que cerrarla siempre.
type Preparation struct {
	preparer *CloudPreparer
	project  *aggregates.Project
	timer    *PhaseTimer
	pull     *backgroundPull // nil si la imagen no se baja de un registro
}

// Begin inicia sesión si hace falta y reconcilia el proyecto con el portal, mientras la imagen de runtime se
// baja en segundo plano. Si el portal corrige el proyecto (su id, o de qué pipeline se trata), project queda
// actualizado y guardado: lo que se clone después es lo que el portal conoce.
func (p *CloudPreparer) Begin(ctx context.Context, project *aggregates.Project, timer *PhaseTimer) (*Preparation, error) {
	prep := &Preparation{preparer: p, project: project, timer: timer}
	prep.startPull(ctx)

	if err := p.ensureLogin(ctx, timer); err != nil {
		prep.Close()
		return nil, err
	}
	if err := p.reconcile(ctx, project, timer); err != nil {
		prep.Close()
		return nil, err
	}
	// La imagen que se estaba bajando puede no ser ya la del proyecto.
	if prep.pull != nil && prep.pull.image != registryImage(project) {
		prep.Close()
		prep.startPull(ctx)
	}
	return prep, nil
}

func (p *CloudPreparer) ensureLogin(ctx context.Context, timer *PhaseTimer) error {
	defer timer.Phase("login")()
	if err := p.login.EnsureToken(ctx); err != nil {
		return fmt.Errorf("iniciar sesión en vex: %w", err)
	}
	return nil
}

// reconcile hace que el portal conozca el proyecto y adopta lo que el portal diga de él.
func (p *CloudPreparer) reconcile(ctx context.Context, project *aggregates.Project, timer *PhaseTimer) error {
	defer timer.Phase("proyecto")()

	resp, err := p.portal.CreateOrGetProject(ctx, buildCreateOrGetRequest(project))
	if errors.Is(err, portalclient.ErrUnauthorized) {
		// La sesión guardada ya no vale para el portal: se inicia otra, una sola vez.
		if err = p.relogin(ctx); err == nil {
			resp, err = p.portal.CreateOrGetProject(ctx, buildCreateOrGetRequest(project))
		}
	}
	switch {
	case errors.Is(err, portalclient.ErrUnauthorized):
		return errors.New("el portal rechazó la sesión de vex. Ejecuta `vex login` y vuelve a intentarlo")
	case errors.Is(err, portalclient.ErrForbidden):
		return errors.New("el portal no te reconoce como miembro de este proyecto: pide acceso a quien lo administra")
	case err != nil:
		return fmt.Errorf("registrar el proyecto en el portal: %w", err)
	}

	if resp.Authoritative != nil {
		changed, err := ApplyAuthoritative(p.projects, project, resp.Authoritative, p.approve)
		var declined *PortalChangeError
		if errors.As(err, &declined) {
			return err
		}
		if err != nil {
			return fmt.Errorf("adoptar los datos del portal: %w", err)
		}
		if changed {
			p.notify.Warn("vexconfig.yaml se actualizó con los datos que el portal tiene de este proyecto")
		}
	}
	if resp.NeedsSync {
		if err := p.portal.SyncPipeline(ctx, resp.PipelineID); err != nil {
			return fmt.Errorf("sincronizar el pipeline con el portal: %w", err)
		}
	}
	return nil
}

func (p *CloudPreparer) relogin(ctx context.Context) error {
	if err := p.login.ForgetToken(); err != nil {
		return err
	}
	if err := p.login.EnsureToken(ctx); err != nil {
		return fmt.Errorf("iniciar sesión en vex: %w", err)
	}
	return nil
}

// Ready comprueba, con el pipeline ya clonado, que el intento puede correr con las credenciales del portal: lee
// el bootstrap del pipeline, busca el adaptador de su contrato, consulta la conexión del ambiente una sola vez
// y exige que esté activa y que se haya creado con ese mismo bootstrap. Después espera a que la imagen esté.
func (p *Preparation) Ready(ctx context.Context, pipeline Source, environment string) (CloudPlan, error) {
	c := p.preparer
	bootstrap, err := c.bootstraps.Read(pipeline.Path, p.project.Cloud().Provider())
	if err != nil {
		return CloudPlan{}, err
	}
	origin, err := c.templateOrigin(p.project, pipeline, bootstrap)
	if err != nil {
		return CloudPlan{}, err
	}
	adapter, err := c.adapters.Lookup(bootstrap.Manifest.Contract)
	if err != nil {
		return CloudPlan{}, err
	}
	reserved := slices.Concat(proVos.NeutralReservedEnvPrefixes, adapter.Workload().ReservedEnvPrefixes())
	if err := ValidateCloudConfig(p.project, reserved); err != nil {
		return CloudPlan{}, err
	}

	conn, err := p.connection(ctx, environment)
	if err != nil {
		return CloudPlan{}, err
	}
	if conn.Status != ConnectionActive {
		return CloudPlan{}, &ConnectionNotReadyError{Environment: environment, Status: conn.Status, Hint: conn.Hint}
	}
	if err := CheckConnection(bootstrap, conn, environment); err != nil {
		return CloudPlan{}, err
	}

	if err := p.waitForImage(ctx); err != nil {
		return CloudPlan{}, err
	}
	if ref := p.project.Pipeline().Ref(); !commitSHAPattern.MatchString(ref) {
		c.notify.Warn(fmt.Sprintf(
			"El pipeline apunta a %q, que puede cambiar sin que lo notes. Fija un commit con \"ref: <sha>\" en vexconfig.yaml.", ref))
	}
	if origin == TemplateUnsigned {
		c.notify.Warn(UnsignedTemplateNotice)
	}
	return CloudPlan{Connection: conn, Bootstrap: bootstrap, Adapter: adapter}, nil
}

var commitSHAPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// connection consulta la conexión del ambiente. Si el portal rechaza la sesión, inicia otra y repite una vez.
func (p *Preparation) connection(ctx context.Context, environment string) (CloudConnectionInfo, error) {
	defer p.timer.Phase("conexion")()
	c := p.preparer
	projectID := p.project.ID().String()

	conn, err := c.conns.CloudConnection(ctx, projectID, environment)
	if errors.Is(err, ErrPortalSessionInvalid) {
		if err = c.relogin(ctx); err == nil {
			conn, err = c.conns.CloudConnection(ctx, projectID, environment)
		}
	}
	switch {
	case errors.Is(err, ErrPortalSessionInvalid):
		return CloudConnectionInfo{}, errors.New("el portal rechazó la sesión de vex. Ejecuta `vex login` y vuelve a intentarlo")
	case err != nil:
		return CloudConnectionInfo{}, fmt.Errorf("consultar la conexión de nube de '%s': %w", environment, err)
	}
	return conn, nil
}

func (p *Preparation) waitForImage(ctx context.Context) error {
	if p.pull == nil {
		return nil
	}
	defer p.timer.Phase("pull_espera")()
	if err := p.pull.wait(ctx); err != nil {
		return fmt.Errorf("no se pudo descargar la imagen de runtime %s: %w", p.pull.image, err)
	}
	return nil
}

// Close detiene la descarga de la imagen si sigue en curso. Es idempotente y acepta una preparación nil.
func (p *Preparation) Close() {
	if p != nil && p.pull != nil {
		p.pull.stop()
		p.pull = nil
	}
}

func (p *Preparation) startPull(ctx context.Context) {
	if image := registryImage(p.project); image != "" && p.preparer.puller != nil {
		p.pull = startPull(ctx, p.preparer.puller, image)
	}
}

// registryImage es la imagen del proyecto si viene de un registro; "" si se construye desde un Dockerfile.
func registryImage(project *aggregates.Project) string {
	if image := project.Runtime().Image(); image.TagExplicit() {
		return image.String()
	}
	return ""
}

// backgroundPull es una descarga de imagen en segundo plano: empieza antes de iniciar sesión para solaparse con
// todo lo que hay que esperar de todos modos.
type backgroundPull struct {
	image  string
	cancel context.CancelFunc
	done   chan error
	waited bool
}

func startPull(ctx context.Context, puller ImagePuller, image string) *backgroundPull {
	pullCtx, cancel := context.WithCancel(ctx)
	pull := &backgroundPull{image: image, cancel: cancel, done: make(chan error, 1)}
	go func() { pull.done <- puller.Pull(pullCtx, image) }()
	return pull
}

// wait espera el resultado de la descarga o a que ctx se cancele.
func (b *backgroundPull) wait(ctx context.Context) error {
	select {
	case err := <-b.done:
		b.waited = true
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// stop cancela la descarga y espera a que su goroutine termine.
func (b *backgroundPull) stop() {
	b.cancel()
	if !b.waited {
		<-b.done
		b.waited = true
	}
}

// beginCloud empieza la preparación si el proyecto usa el portal. Sin portal (o sin preparador) devuelve nil,
// que es una preparación vacía: Close y readyCloud la aceptan.
func beginCloud(ctx context.Context, preparer *CloudPreparer, project *aggregates.Project, timer *PhaseTimer) (*Preparation, error) {
	if preparer == nil || !project.Cloud().IsPortal() {
		return nil, nil
	}
	return preparer.Begin(ctx, project, timer)
}

// readyCloud termina la preparación y anuncia con qué conexión corre el intento. Sin preparación devuelve el
// plan vacío.
func readyCloud(
	ctx context.Context, preparation *Preparation, pipeline Source, environment string, presenter ExecutionPresenter,
) (CloudPlan, error) {
	if preparation == nil {
		return CloudPlan{}, nil
	}
	plan, err := preparation.Ready(ctx, pipeline, environment)
	if err != nil {
		return CloudPlan{}, err
	}
	presenter.CloudConnected(CloudNotice{
		Environment: environment, Contract: plan.Bootstrap.Manifest.Contract,
		ConnectionID: plan.Connection.ConnectionID, Hints: plan.Adapter.Workload().ErrorHints(),
	})
	return plan, nil
}
