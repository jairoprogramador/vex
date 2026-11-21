package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jairoprogramador/vex/internal/domain/project/aggregates"
)

// Errores de `vex bootstrap` que quien llama distingue.
var (
	// ErrBootstrapNeedsPortal: el proyecto no usa las credenciales del portal, así que no hay nada que conectar.
	ErrBootstrapNeedsPortal = errors.New("este proyecto no usa las credenciales del portal")
	// ErrBootstrapNotAccepted: se pidió aplicar sin la confirmación que el plan exige.
	ErrBootstrapNotAccepted = errors.New("el bootstrap no se aceptó")
	// ErrBootstrapChanged: el bootstrap del pipeline cambió entre la vista previa y la aplicación.
	ErrBootstrapChanged = errors.New("el bootstrap cambió después de la vista previa")
	// ErrVerificationPending: la conexión quedó verificándose y no llegó a activarse en el plazo.
	ErrVerificationPending = errors.New("la conexión aún no se pudo verificar")
	// ErrIssuerNotConfigured: este binario no sabe en qué emisor confiar, así que no crea confianzas.
	ErrIssuerNotConfigured = errors.New("este binario de vex no trae configurado el emisor de tokens")
)

// VerificationFailedError: el portal probó la conexión y no funciona.
type VerificationFailedError struct {
	Environment string
	Message     string
	Hint        string
}

func (e *VerificationFailedError) Error() string {
	text := fmt.Sprintf("La conexión de nube de '%s' no pasó la verificación: %s", e.Environment, e.Message)
	// El portal suele decir ya qué ejecutar; solo se añade si no lo dijo.
	if advice := bootstrapAdvice(e.Environment, e.Hint); !strings.Contains(e.Message, advice) {
		text += " " + advice
	}
	return text
}

// BootstrapMode es qué va a hacer `vex bootstrap` con el ambiente.
type BootstrapMode int

const (
	// BootstrapUpToDate: ya hay una conexión activa creada con este mismo bootstrap. No se hace nada.
	BootstrapUpToDate BootstrapMode = iota
	// BootstrapResume: la conexión ya se aplicó y quedó verificándose; solo falta verificarla.
	BootstrapResume
	// BootstrapConnect: no hay conexión vigente; se crea.
	BootstrapConnect
	// BootstrapReconnect: se sustituye la conexión vigente, que sigue activa hasta que la nueva se verifica.
	BootstrapReconnect
)

// BootstrapOptions son las opciones del comando.
type BootstrapOptions struct {
	Environment string
	// Region es dónde aplicar; vacío usa la de la conexión que ya existe.
	Region string
	// Reconnect rehace la conexión aunque el bootstrap no haya cambiado.
	Reconnect bool
}

// BootstrapPlan es lo que la persona tiene que ver antes de aceptar: con qué cuenta, sobre qué proyecto, qué
// pipeline y qué cambiaría. Lo arma Plan y lo consume Apply.
type BootstrapPlan struct {
	Environment string
	Mode        BootstrapMode
	Contract    ContractID
	Account     []LabeledValue
	ProjectName string
	Owner       ProjectOwner
	IsOwner     bool
	// Resumed: el portal ya tenía esta conexión a medias; no se abrió otra.
	Resumed        bool
	PipelineURL    string
	PipelineCommit string
	Fingerprint    string
	// Origin dice si el catálogo firmado de vex respalda este bootstrap o si lo revisa solo quien lo acepta.
	Origin     TemplateOrigin
	CloudSteps []string
	Providers  []string
	Region     string
	Preview    BootstrapPreview

	project      *aggregates.Project
	adapter      CloudAdapter
	pipelineDir  string
	request      BootstrapRequest
	connectionID string
	trust        Trust
}

// NeedsAcceptance dice si el plan aplica algo en la nube y por tanto hay que aceptarlo.
func (p *BootstrapPlan) NeedsAcceptance() bool {
	return p.Mode == BootstrapConnect || p.Mode == BootstrapReconnect
}

// TypedConfirmation es el texto que hay que teclear para aceptar un plan con permisos resaltados o con recursos
// que la nube no supo previsualizar: el nombre del ambiente. Vacío si basta con responder sí.
func (p *BootstrapPlan) TypedConfirmation() string {
	if len(p.Preview.ElevatedGrants()) == 0 && len(p.Preview.Unpreviewed()) == 0 {
		return ""
	}
	return p.Environment
}

// Acceptance es la respuesta de la persona al plan.
type Acceptance struct {
	Confirmed bool
	// Typed es lo que tecleó cuando el plan pedía confirmación escrita.
	Typed string
	// ConfirmExposure pregunta, ya aplicada la plantilla y antes de crear la confianza, si se aceptan los permisos
	// que la identidad de despliegue tiene sin que la vista previa los mostrara. Devuelve lo tecleado. Sin esta
	// función, esos permisos no se aceptan.
	ConfirmExposure func(grants []PlannedGrant) (string, error)
}

// BootstrapResult es cómo quedó el ambiente.
type BootstrapResult struct {
	Environment  string
	ConnectionID string
	Elapsed      time.Duration
}

// verifyBudget es cuánto se espera a que la nube reconozca la confianza recién creada; el portal da por
// perdida una verificación a los tres minutos.
const (
	verifyBudget     = 5 * time.Minute
	verifyDefaultGap = 10 * time.Second
	verifyMaxGap     = time.Minute
)

// BootstrapService conecta la nube de quien lo ejecuta con vex: aplica el bootstrap del pipeline con su sesión,
// crea la confianza y deja la conexión verificada. No conoce ninguna nube: todo lo específico está detrás del
// adaptador del contrato.
type BootstrapService struct {
	workspace  *Workspace
	preparer   *CloudPreparer
	bootstraps BootstrapReader
	adapters   CloudAdapters
	admin      CloudConnectionAdmin
	journal    BootstrapJournal
	notify     Notifier
	// issuer es el emisor de tokens en el que confía este binario. No se toma del portal: un portal comprometido
	// no puede apuntar la confianza a otro emisor.
	issuer string
	now    func() time.Time
	sleep  func(ctx context.Context, d time.Duration) error
}

func NewBootstrapService(
	workspace *Workspace, preparer *CloudPreparer, bootstraps BootstrapReader, adapters CloudAdapters,
	admin CloudConnectionAdmin, journal BootstrapJournal, notify Notifier, issuer string,
) *BootstrapService {
	return &BootstrapService{
		workspace: workspace, preparer: preparer, bootstraps: bootstraps, adapters: adapters,
		admin: admin, journal: journal, notify: notify, issuer: issuer,
		now: time.Now, sleep: sleepContext,
	}
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// cloudSetup es lo que hace falta saber antes de tocar una conexión: el proyecto, el bootstrap del pipeline en
// un commit concreto, el adaptador de su contrato, la sesión de la nube y la conexión actual del ambiente.
type cloudSetup struct {
	project   *aggregates.Project
	pipeline  Source
	bootstrap PipelineBootstrap
	origin    TemplateOrigin
	adapter   CloudAdapter
	account   CloudAccount
	conn      CloudConnectionInfo
}

// portalProject lee el proyecto y exige que use las credenciales del portal, con sesión de vex iniciada y el
// proyecto registrado. Devuelve la preparación abierta: hay que cerrarla.
func portalProject(ctx context.Context, workspace *Workspace, preparer *CloudPreparer, environment string) (*aggregates.Project, *Preparation, error) {
	if !ValidEnvironmentName(environment) {
		return nil, nil, fmt.Errorf("%q no es un nombre de ambiente válido (minúsculas, números y guiones)", environment)
	}
	project, err := workspace.LoadProject()
	if err != nil {
		return nil, nil, err
	}
	if !project.Cloud().IsPortal() {
		return nil, nil, ErrBootstrapNeedsPortal
	}
	preparation, err := preparer.Begin(ctx, project, nil)
	if err != nil {
		return nil, nil, err
	}
	return project, preparation, nil
}

func (s *BootstrapService) open(ctx context.Context, environment string) (*cloudSetup, error) {
	if s.issuer == "" {
		return nil, ErrIssuerNotConfigured
	}
	project, preparation, err := portalProject(ctx, s.workspace, s.preparer, environment)
	if err != nil {
		return nil, err
	}
	defer preparation.Close()

	pipeline, err := s.workspace.ClonePipeline(ctx, project)
	if err != nil {
		return nil, err
	}
	bootstrap, err := s.bootstraps.Read(pipeline.Path, project.Cloud().Provider())
	if err != nil {
		return nil, err
	}
	// Antes de mirar nada más de la plantilla: si es del catálogo firmado, tiene que ser la que él publica.
	origin, err := s.preparer.templateOrigin(project, pipeline, bootstrap)
	if err != nil {
		return nil, err
	}
	adapter, err := s.adapters.Lookup(bootstrap.Manifest.Contract)
	if err != nil {
		return nil, err
	}
	// Las reglas estáticas van antes de hablar con la nube o de abrir nada en el portal.
	if err := adapter.Bootstrap().ValidateTemplate(bootstrap); err != nil {
		return nil, err
	}
	account, err := adapter.Bootstrap().Account(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := preparation.connection(ctx, environment)
	if err != nil {
		return nil, err
	}
	return &cloudSetup{
		project: project, pipeline: pipeline, bootstrap: bootstrap, origin: origin,
		adapter: adapter, account: account, conn: conn,
	}, nil
}

// Plan prepara el bootstrap de un ambiente hasta la vista previa. Abre (o retoma) la conexión en el portal y
// registra los servicios que el manifiesto pide; no aplica la plantilla ni crea ninguna confianza.
func (s *BootstrapService) Plan(ctx context.Context, opts BootstrapOptions) (*BootstrapPlan, error) {
	setup, err := s.open(ctx, opts.Environment)
	if err != nil {
		return nil, err
	}
	project, manifest := setup.project, setup.bootstrap.Manifest
	plan := &BootstrapPlan{
		Environment: opts.Environment, Mode: bootstrapMode(setup, opts), Contract: manifest.Contract,
		Account: setup.account.Summary, ProjectName: project.Data().Name(),
		PipelineURL: project.Pipeline().URL(), PipelineCommit: setup.pipeline.Commit,
		Fingerprint: setup.bootstrap.Fingerprint, Origin: setup.origin,
		CloudSteps: manifest.CloudSteps, Providers: manifest.Providers,
		project: project, adapter: setup.adapter, pipelineDir: setup.pipeline.Path, connectionID: setup.conn.ConnectionID,
	}
	if !plan.NeedsAcceptance() {
		return plan, nil
	}

	provider := setup.adapter.Bootstrap()
	if plan.Region, err = provider.Region(opts.Region, setup.conn.Outputs); err != nil {
		return nil, err
	}
	created, err := s.admin.Create(ctx, ConnectionDraft{
		ProjectID: project.ID().String(), Environment: opts.Environment, Contract: manifest.Contract,
		BootstrapSHA256: setup.bootstrap.Fingerprint, PipelineCommit: setup.pipeline.Commit,
		Reconnect: plan.Mode == BootstrapReconnect, Target: setup.account.Target,
	})
	if err != nil {
		return nil, fmt.Errorf("abrir la conexión en el portal: %w", forEnvironment(err, opts.Environment))
	}
	if err := s.adopt(plan, created, setup.adapter.Trust().Audience()); err != nil {
		return nil, err
	}
	plan.request = BootstrapRequest{
		Account: setup.account, Bootstrap: setup.bootstrap,
		Name:      BootstrapOperationName(created.Project.Slug, opts.Environment),
		ProjectID: project.ID().String(), ProjectSlug: created.Project.Slug,
		Environment: opts.Environment, Region: plan.Region,
	}

	if len(manifest.Providers) > 0 {
		s.notify.Info("Registrando los servicios que pide el pipeline (puede tardar unos minutos)…")
		if err := provider.RegisterProviders(ctx, setup.account, manifest.Providers); err != nil {
			return nil, s.interrupted(ctx, opts.Environment, err)
		}
	}
	s.notify.Info("Calculando la vista previa…")
	if plan.Preview, err = provider.Preview(ctx, plan.request); err != nil {
		return nil, s.interrupted(ctx, opts.Environment, err)
	}
	return plan, nil
}

// bootstrapMode decide qué hacer según la conexión actual. Una conexión vigente (activa o verificándose) solo se
// sustituye si el bootstrap cambió o si se pidió expresamente.
func bootstrapMode(setup *cloudSetup, opts BootstrapOptions) BootstrapMode {
	conn := setup.conn
	current := conn.Status == ConnectionActive || conn.Status == VerificationVerifying
	if !current {
		return BootstrapConnect
	}
	if opts.Reconnect || conn.BootstrapSHA256 != setup.bootstrap.Fingerprint {
		return BootstrapReconnect
	}
	if conn.Status == VerificationVerifying {
		return BootstrapResume
	}
	if conn.Outputs == nil || conn.Outputs.ContractID != setup.bootstrap.Manifest.Contract ||
		!setup.account.SameTarget(conn.Outputs.Params) {
		// Otro contrato u otro destino que el de la sesión: no es «al día», es otra conexión.
		return BootstrapReconnect
	}
	return BootstrapUpToDate
}

// adopt comprueba lo que el portal fijó al abrir la conexión y lo deja en el plan. Nada de lo que decide la
// confianza se acepta sin contrastar: el emisor es el del binario, la audiencia es la del contrato y el subject
// y el nombre corto se recalculan aquí.
func (s *BootstrapService) adopt(plan *BootstrapPlan, created CreatedConnection, audience string) error {
	projectID := plan.project.ID().String()
	subject, err := ConnectionSubject(projectID, plan.Environment, created.ConnectionID)
	switch {
	case err != nil:
		return fmt.Errorf("el portal devolvió una conexión que no se puede usar: %w", err)
	case created.Issuer != s.issuer:
		return errors.New("el portal propone un emisor de tokens distinto del de este binario de vex: no se continúa. " +
			"Comprueba que tu vex es el oficial y está actualizado")
	case created.Audience != audience:
		return errors.New("el portal propone una audiencia distinta de la que exige el contrato de nube: no se continúa")
	case created.Subject != subject:
		return errors.New("el portal devolvió un subject que no corresponde a este proyecto y ambiente: no se continúa")
	case created.Project.ID != projectID || created.Project.Slug != ProjectSlug(created.Project.Name, projectID):
		return errors.New("el portal devolvió un proyecto que no corresponde al de vexconfig.yaml: no se continúa")
	}
	plan.connectionID = created.ConnectionID
	plan.Resumed = created.Resumed
	plan.ProjectName = created.Project.Name
	plan.Owner, plan.IsOwner = created.Owner, created.IsOwner
	plan.trust = Trust{Issuer: s.issuer, Subject: subject, Audience: audience}
	return nil
}

// Apply aplica un plan aceptado: plantilla, confianza y verificación. Con un plan que no necesita aplicar nada,
// solo verifica (BootstrapResume) o no hace nada (BootstrapUpToDate).
func (s *BootstrapService) Apply(ctx context.Context, plan *BootstrapPlan, acceptance Acceptance) (BootstrapResult, error) {
	started := s.now()
	result := BootstrapResult{Environment: plan.Environment, ConnectionID: plan.connectionID}
	switch plan.Mode {
	case BootstrapUpToDate:
		return result, nil
	case BootstrapResume:
		err := s.activate(ctx, plan.Environment, func() (Verification, error) {
			return s.admin.Verify(ctx, plan.connectionID)
		})
		result.Elapsed = s.now().Sub(started)
		return result, err
	}

	if err := checkAcceptance(plan, acceptance); err != nil {
		return result, err
	}
	// La huella se recalcula justo antes de aplicar: lo que se aplica es lo que se vio.
	current, err := s.bootstraps.Read(plan.pipelineDir, plan.request.Bootstrap.Cloud)
	if err != nil {
		return result, fmt.Errorf("%w: %w", ErrBootstrapChanged, err)
	}
	if current.Fingerprint != plan.Fingerprint {
		return result, fmt.Errorf("%w.\n→ No se aplicó nada. Vuelve a ejecutar `vex bootstrap %s`", ErrBootstrapChanged, plan.Environment)
	}
	// Sin constancia de lo aceptado no se aplica.
	if err := s.journal.Record(s.record(plan, "accepted")); err != nil {
		return result, fmt.Errorf("guardar la constancia de lo aceptado: %w", err)
	}

	s.notify.Info("Aplicando el bootstrap (puede tardar varios minutos)…")
	outputs, err := plan.adapter.Bootstrap().Apply(ctx, plan.request, plan.Preview.Seal)
	if err != nil {
		return result, s.interrupted(ctx, plan.Environment, err)
	}
	// Antes de confiar: la identidad tiene que ser del bootstrap y no traer permisos que nadie vio.
	trust := plan.adapter.Trust()
	exposed, err := trust.Exposure(ctx, plan.request.Account, plan.Preview, outputs, plan.trust)
	if err != nil {
		return result, s.interrupted(ctx, plan.Environment, err)
	}
	if err := s.acceptExposure(plan, acceptance, exposed); err != nil {
		return result, err
	}
	s.notify.Info("Creando la confianza de vex sobre la identidad de despliegue…")
	if err := trust.Ensure(ctx, plan.request.Account, outputs, plan.trust); err != nil {
		return result, s.interrupted(ctx, plan.Environment, err)
	}

	s.notify.Info("Verificando la conexión con un canje real…")
	completion := ConnectionCompletion{
		ConnectionID: plan.connectionID, Target: plan.request.Account.Target,
		BootstrapSHA256: plan.Fingerprint, Outputs: outputs,
	}
	// Complete es idempotente: repetirlo retoma la verificación sin volver a registrar nada.
	err = s.activate(ctx, plan.Environment, func() (Verification, error) { return s.admin.Complete(ctx, completion) })
	result.Elapsed = s.now().Sub(started)
	outcome := "active"
	if err != nil {
		outcome = "not_verified"
	}
	if recordErr := s.journal.Record(s.record(plan, outcome)); recordErr != nil {
		s.notify.Warn("No se pudo guardar la constancia local del resultado: " + recordErr.Error())
	}
	return result, err
}

// acceptExposure exige la aceptación escrita de los permisos que la identidad de despliegue tiene y la vista previa
// no mostró. Sin ella no se crea la confianza: lo aplicado queda en la nube, pero vex no puede usarlo.
func (s *BootstrapService) acceptExposure(plan *BootstrapPlan, acceptance Acceptance, exposed []PlannedGrant) error {
	if len(exposed) == 0 {
		return nil
	}
	typed := ""
	if acceptance.ConfirmExposure != nil {
		var err error
		if typed, err = acceptance.ConfirmExposure(exposed); err != nil {
			return err
		}
	}
	record := s.record(plan, "exposure_accepted")
	record.ExposedGrants = exposed
	if typed != plan.Environment {
		record.Outcome = "exposure_declined"
	}
	if err := s.journal.Record(record); err != nil {
		return fmt.Errorf("guardar la constancia de lo aceptado: %w", err)
	}
	if typed != plan.Environment {
		return fmt.Errorf("%w: la identidad de despliegue tiene permisos que la vista previa no mostró y solo se aceptan "+
			"tecleando %q.\n→ La plantilla ya se aplicó, pero vex no creó su confianza: nadie puede usar esos permisos a través "+
			"de vex. Revisa la identidad y vuelve a ejecutar `vex bootstrap %s`", ErrBootstrapNotAccepted, plan.Environment, plan.Environment)
	}
	return nil
}

func checkAcceptance(plan *BootstrapPlan, acceptance Acceptance) error {
	if typed := plan.TypedConfirmation(); typed != "" {
		if acceptance.Typed != typed {
			return fmt.Errorf("%w: los permisos resaltados y lo que la nube no supo previsualizar solo se aceptan tecleando %q",
				ErrBootstrapNotAccepted, typed)
		}
		return nil
	}
	if !acceptance.Confirmed {
		return ErrBootstrapNotAccepted
	}
	return nil
}

func (s *BootstrapService) record(plan *BootstrapPlan, outcome string) AcceptanceRecord {
	return AcceptanceRecord{
		At: s.now().UTC(), Action: "bootstrap", Outcome: outcome,
		ProjectID: plan.project.ID().String(), Environment: plan.Environment, Contract: string(plan.Contract),
		ConnectionID: plan.connectionID, Account: plan.Account,
		PipelineURL: plan.PipelineURL, PipelineCommit: plan.PipelineCommit, Fingerprint: plan.Fingerprint,
		Region: plan.Region, ElevatedGrants: plan.Preview.ElevatedGrants(), Unpreviewed: len(plan.Preview.Unpreviewed()),
	}
}

// activate pregunta al portal hasta que la conexión queda verificada, falla o se agota el plazo. La nube tarda en
// reconocer una confianza recién creada: mientras tanto el portal responde «verificándose» o pide esperar.
func (s *BootstrapService) activate(ctx context.Context, environment string, ask func() (Verification, error)) error {
	deadline := s.now().Add(verifyBudget)
	for {
		verification, err := ask()
		wait, err := s.nextAttempt(environment, verification, err)
		if err != nil || wait == 0 {
			return s.interrupted(ctx, environment, err)
		}
		if s.now().Add(wait).After(deadline) {
			return fmt.Errorf("%w tras %s.\n→ Lo aplicado en la nube sigue ahí: vuelve a ejecutar `vex bootstrap %s` para retomarlo",
				ErrVerificationPending, verifyBudget, environment)
		}
		if err := s.sleep(ctx, wait); err != nil {
			return s.interrupted(ctx, environment, err)
		}
	}
}

// nextAttempt decide, tras una respuesta del portal, si ya está (0, nil), si hay que esperar (wait) o si no tiene
// arreglo (error).
func (s *BootstrapService) nextAttempt(environment string, v Verification, err error) (time.Duration, error) {
	var rejected *ConnectionRejectedError
	switch {
	case err == nil && v.Conclusive():
		return 0, nil
	case err == nil && v.Status == VerificationFailed:
		return 0, &VerificationFailedError{Environment: environment, Message: v.Message, Hint: v.Hint}
	case err == nil:
		s.notify.Info("  aún no: " + v.Message)
		return clampGap(v.RetryAfter), nil
	case errors.Is(err, ErrCloudTokenThrottled):
		var retry RetryAfter
		wait := time.Duration(0)
		if errors.As(err, &retry) {
			wait = retry.RetryAfter()
		}
		s.notify.Info("  el portal pide esperar antes de volver a verificar…")
		return clampGap(wait), nil
	case errors.As(err, &rejected):
		if rejected.Code == RejectionConnectionInProgress || rejected.Code == RejectionSignerUnavailable {
			s.notify.Info("  aún no: " + rejected.Error())
			return verifyDefaultGap, nil
		}
		return 0, forEnvironment(err, environment)
	case errors.Is(err, ErrCloudTokenDenied), errors.Is(err, context.Canceled):
		return 0, err
	default:
		// Un corte de red no deshace nada: se vuelve a preguntar.
		s.notify.Warn("No se pudo consultar al portal; se reintenta: " + err.Error())
		return verifyDefaultGap, nil
	}
}

func clampGap(wait time.Duration) time.Duration {
	if wait <= 0 {
		return verifyDefaultGap
	}
	return min(max(wait, time.Second), verifyMaxGap)
}

// interrupted explica cómo seguir cuando Ctrl+C corta el bootstrap a medias: el portal conserva la conexión
// abierta y volver a ejecutar el comando la retoma.
func (s *BootstrapService) interrupted(ctx context.Context, environment string, err error) error {
	if err != nil && ctx.Err() != nil {
		return fmt.Errorf("interrumpido: la conexión de '%s' quedó a medias. Vuelve a ejecutar `vex bootstrap %s` para retomarla (%w)",
			environment, environment, context.Canceled)
	}
	return err
}
