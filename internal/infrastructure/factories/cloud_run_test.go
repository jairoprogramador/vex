package factories

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	app "github.com/jairoprogramador/vex/internal/application"
	comVos "github.com/jairoprogramador/vex/internal/domain/common/vos"
	"github.com/jairoprogramador/vex/internal/domain/project/aggregates"
	proVos "github.com/jairoprogramador/vex/internal/domain/project/vos"
	"github.com/jairoprogramador/vex/internal/infrastructure/bootstrap"
	"github.com/jairoprogramador/vex/internal/infrastructure/cloudsession"
	"github.com/jairoprogramador/vex/internal/infrastructure/portalclient"
)

// Un `vex run` con cloud.credentials: portal de punta a punta, con las piezas reales (lector del bootstrap,
// registro de adaptadores, sesión y archivo del token en disco, ejecutor) y falsos solo en los bordes: el
// portal, git y el motor. El motor falso mira el disco en cada evento, como lo vería un comando del paso.

const testJWT = "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJ2ZXg6cDoxIn0.ZmlybWEtZGUtcHJ1ZWJh"

type e2eProjects struct{ project *aggregates.Project }

func (r *e2eProjects) Save(*aggregates.Project) error     { return nil }
func (r *e2eProjects) Exists() (bool, error)              { return true, nil }
func (r *e2eProjects) Load() (*aggregates.Project, error) { return r.project, nil }

// e2eSources «clona» devolviendo carpetas ya preparadas.
type e2eSources struct{ project, pipeline string }

func (s e2eSources) Ensure(_ context.Context, url, _ string) (app.Source, error) {
	if strings.Contains(url, "pipe") {
		return app.Source{Path: s.pipeline, Commit: "c-pipe"}, nil
	}
	return app.Source{Path: s.project, Commit: "c-app"}, nil
}

type e2eImages struct{}

func (e2eImages) Build(context.Context, app.ImageBuild) error { return nil }

type e2ePresenter struct{ warnings, infos []string }

func (p *e2ePresenter) Info(m string)                       { p.infos = append(p.infos, m) }
func (p *e2ePresenter) Warn(m string)                       { p.warnings = append(p.warnings, m) }
func (p *e2ePresenter) CloudConnected(app.CloudNotice)      {}
func (p *e2ePresenter) Event(app.EngineEvent)               {}
func (p *e2ePresenter) Result(app.AttemptResult)            {}
func (p *e2ePresenter) Failure(string, []app.CommandOutput) {}
func (p *e2ePresenter) all() string                         { return strings.Join(append(p.infos, p.warnings...), "\n") }

type e2eLogin struct{}

func (e2eLogin) EnsureToken(context.Context) error { return nil }
func (e2eLogin) ForgetToken() error                { return nil }

// e2ePortal es el portal: conoce el proyecto, tiene una conexión y emite tokens.
type e2ePortal struct {
	connection app.CloudConnectionInfo
	issueErr   error
	issued     int
}

func (p *e2ePortal) CreateOrGetProject(context.Context, portalclient.CreateOrGetProjectRequest) (portalclient.CreateOrGetProjectResponse, error) {
	return portalclient.CreateOrGetProjectResponse{PipelineID: "pl-1"}, nil
}
func (p *e2ePortal) SyncPipeline(context.Context, string) error { return nil }
func (p *e2ePortal) CloudConnection(context.Context, string, string) (app.CloudConnectionInfo, error) {
	return p.connection, nil
}
func (p *e2ePortal) Issue(context.Context, app.TokenRequest) (app.IssuedToken, error) {
	if p.issueErr != nil {
		return app.IssuedToken{}, p.issueErr
	}
	p.issued++
	now := time.Now()
	return app.IssuedToken{Value: testJWT, ExpiresAt: now.Add(10 * time.Minute), RefreshAfter: now.Add(5 * time.Minute)}, nil
}

// e2eEngine emite los eventos programados y anota, tras cada uno, qué hay en el directorio montado.
type e2eEngine struct {
	events   []app.EngineEvent
	onEvent  func(index int) // gancho opcional tras cada evento
	seen     []string
	spec     app.ContainerSpec
	request  app.AttemptRequest
	attempts int
	credDir  string
}

func (e *e2eEngine) Attempt(ctx context.Context, spec app.ContainerSpec, req app.AttemptRequest, onEvent func(app.EngineEvent)) (app.AttemptResult, error) {
	e.attempts++
	e.spec, e.request = spec, req
	for _, mount := range spec.Mounts {
		if mount.Target == app.ContainerCredentialsDir {
			e.credDir = mount.Source
		}
	}
	onEvent(app.EngineEvent{Kind: app.AttemptStarted, AttemptID: "i1"})
	for i, event := range e.events {
		if ctx.Err() != nil {
			return app.AttemptResult{AttemptID: "i1", Status: app.AttemptCanceled}, nil
		}
		onEvent(event)
		e.seen = append(e.seen, fmt.Sprintf("%s:%s %s", event.Kind, event.Step, e.tokenOnDisk()))
		if e.onEvent != nil {
			e.onEvent(i)
		}
	}
	if ctx.Err() != nil {
		return app.AttemptResult{AttemptID: "i1", Status: app.AttemptCanceled}, nil
	}
	return app.AttemptResult{AttemptID: "i1", Status: app.AttemptSucceeded}, nil
}

func (e *e2eEngine) Logs(context.Context, app.ContainerSpec, app.LogsRequest) ([]app.CommandOutput, error) {
	return nil, nil
}

// tokenOnDisk dice qué vería el contenedor en /vex/credenciales.
func (e *e2eEngine) tokenOnDisk() string {
	entries, err := os.ReadDir(e.credDir)
	if err != nil {
		return "sin-directorio"
	}
	if len(entries) == 0 {
		return "vacío"
	}
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name()
	}
	return strings.Join(names, ",")
}

type e2e struct {
	executor  *app.LocalExecutorService
	engine    *e2eEngine
	portal    *e2ePortal
	presenter *e2ePresenter
	credBase  string
}

func newE2E(t *testing.T, events ...app.EngineEvent) *e2e {
	t.Helper()
	base := t.TempDir()
	pipeline := filepath.Join(base, "pipe")
	for name, content := range map[string]string{
		"bootstrap/azure/bootstrap.yaml": "contract: azure/v1\ntemplate: main.json\ncloud_steps: [supply, deploy]\n",
		"bootstrap/azure/main.json":      "{}\n",
		"bootstrap/azure/main.bicep":     "targetScope = 'subscription'\n",
	} {
		full := filepath.Join(pipeline, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
	accepted, err := bootstrap.Reader{}.Read(pipeline, "")
	require.NoError(t, err)

	const sub = "22222222-2222-4222-8222-222222222222"
	portal := &e2ePortal{connection: app.CloudConnectionInfo{
		Status: app.ConnectionActive, ConnectionID: "c0ffee00-0000-4000-8000-0000000000c1", BootstrapSHA256: accepted.Fingerprint,
		Outputs: &app.ConnectionOutputs{
			ContractID: "azure/v1",
			Params: map[string]string{
				"tenant_id": "11111111-1111-4111-8111-111111111111", "subscription_id": sub,
				"deploy_client_id": "33333333-3333-4333-8333-333333333333", "deploy_principal_id": "44444444-4444-4444-8444-444444444444",
				"deploy_identity_id": "/subscriptions/" + sub + "/resourceGroups/rg-id/providers/Microsoft.ManagedIdentity/userAssignedIdentities/id-deploy",
				"resource_group":     "rg-pagos-sand", "location": "eastus2",
			},
			Extras: map[string]string{"registryLoginServer": "crpagossand.azurecr.io"},
		},
	}}

	id, err := proVos.NewProjectID("seed-id")
	require.NoError(t, err)
	data, err := proVos.NewProjectData("Demo", "vex", "equipo", "desc", "https://github.com/local/app.git", "main")
	require.NoError(t, err)
	pipe, err := comVos.NewPipeline("https://github.com/local/pipe.git", "0123456789abcdef0123456789abcdef01234567")
	require.NoError(t, err)
	image, err := comVos.NewImage("docker/Dockerfile") // se construye: no hay nada que bajar
	require.NoError(t, err)
	project, err := aggregates.NewProject(id, data, pipe, proVos.NewRuntime(proVos.WithImage(image)))
	require.NoError(t, err)
	cloud, err := proVos.NewCloud("", proVos.CredentialsPortal)
	require.NoError(t, err)
	project.SetCloud(cloud)

	presenter := &e2ePresenter{}
	projects := &e2eProjects{project: project}
	engine := &e2eEngine{events: events}
	credBase := filepath.Join(base, "cache", "credenciales")
	adapters := cloudAdapters()
	workspace := app.NewWorkspace(projects, e2eSources{project: filepath.Join(base, "app"), pipeline: pipeline}, e2eImages{}, presenter,
		app.LocalExecutorConfig{
			WorkDir: base, StoreDir: filepath.Join(base, "almacen"),
			SpaceDir: filepath.Join(base, "cache", "espacio"), MaterialDir: filepath.Join(base, "cache", "material"),
		})
	executor := app.NewLocalExecutorService(workspace, engine, presenter,
		app.WithPortalCredentials(cloudsession.NewCredentials(portal, portal, adapters, credBase, presenter.Warn)),
		app.WithCloudPreparer(app.NewCloudPreparer(e2eLogin{}, portal, projects, portal, bootstrap.Reader{}, adapters, nil, presenter)))
	return &e2e{executor: executor, engine: engine, portal: portal, presenter: presenter, credBase: credBase}
}

func step(kind app.EngineEventKind, name string) app.EngineEvent {
	return app.EngineEvent{Kind: kind, Step: name, StepStatus: app.StepExecuted}
}

func (e *e2e) leftovers(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(e.credBase)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	require.NoError(t, err)
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestPortal_ElTokenSoloExisteEnDiscoDuranteLosPasosDeNube(t *testing.T) {
	defer goleak.VerifyNone(t)
	e := newE2E(t,
		app.EngineEvent{Kind: app.StepFinished, Step: "test", StepStatus: app.StepReused},
		step(app.StepStarted, "supply"), step(app.CommandFinished, "supply"), step(app.StepFinished, "supply"),
		step(app.StepStarted, "package"), step(app.CommandFinished, "package"), step(app.StepFinished, "package"),
		step(app.StepStarted, "deploy"), step(app.CommandFinished, "deploy"), step(app.StepFinished, "deploy"),
	)

	require.NoError(t, e.executor.Run(context.Background(), "deploy", "sand"))

	assert.Equal(t, []string{
		"step_finished:test vacío",
		"step_started:supply oidc.jwt", "command_finished:supply oidc.jwt", "step_finished:supply vacío",
		"step_started:package vacío", "command_finished:package vacío", "step_finished:package vacío",
		"step_started:deploy oidc.jwt", "command_finished:deploy oidc.jwt", "step_finished:deploy vacío",
	}, e.engine.seen, "package no está en cloud_steps: no ve el token")
	assert.Equal(t, 2, e.portal.issued, "una emisión por paso de nube ejecutado")
	assert.Empty(t, e.leftovers(t), "al terminar no queda ningún directorio de credenciales")

	// El directorio de credenciales va aparte del espacio de trabajo y se monta de solo lectura.
	var credentials app.Mount
	for _, mount := range e.engine.spec.Mounts {
		assert.False(t, mount.Target != app.ContainerCredentialsDir && strings.HasPrefix(e.engine.credDir, mount.Source+string(filepath.Separator)),
			"el directorio de credenciales no puede colgar de %s", mount.Target)
		if mount.Target == app.ContainerCredentialsDir {
			credentials = mount
		}
	}
	assert.True(t, credentials.ReadOnly)
	assert.Equal(t, e.credBase, filepath.Dir(credentials.Source))

	// El contrato llega por el entorno de la petición; el token, por ningún sitio que no sea el archivo.
	env := e.engine.request.Secrets
	assert.Equal(t, "azure/v1", env["VEX_CLOUD_CONTRACT"])
	assert.Equal(t, "/vex/credenciales/oidc.jwt", env["AZURE_FEDERATED_TOKEN_FILE"])
	assert.Equal(t, "crpagossand.azurecr.io", env["VEX_BOOTSTRAP_REGISTRY_LOGIN_SERVER"])
	for name, value := range env {
		assert.NotContains(t, value, "eyJ", "%s lleva algo con forma de token", name)
	}
	for _, variable := range e.engine.spec.Env {
		assert.NotContains(t, variable.Value, "eyJ")
	}
	assert.Regexp(t, `^azure/v1:[0-9a-f]{16}$`, e.engine.request.Context)
	assert.NotContains(t, e.presenter.all(), testJWT, "el token no aparece en ningún mensaje")
}

func TestPortal_SiElMotorPierdeEventosElTokenNoQuedaEnDisco(t *testing.T) {
	t.Run("se pierde el final de un paso de nube", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		e := newE2E(t,
			step(app.StepStarted, "supply"), step(app.CommandFinished, "supply"),
			step(app.StepStarted, "smoke"), step(app.CommandFinished, "smoke"), step(app.StepFinished, "smoke"))

		require.NoError(t, e.executor.Run(context.Background(), "smoke", "sand"))

		assert.Equal(t, []string{
			"step_started:supply oidc.jwt", "command_finished:supply oidc.jwt",
			"step_started:smoke vacío", "command_finished:smoke vacío", "step_finished:smoke vacío",
		}, e.engine.seen)
		assert.Empty(t, e.leftovers(t))
	})
	t.Run("se pierde el comienzo de un paso de nube", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		e := newE2E(t, step(app.CommandFinished, "deploy"), step(app.StepFinished, "deploy"))

		require.NoError(t, e.executor.Run(context.Background(), "deploy", "sand"))

		assert.Equal(t, []string{"command_finished:deploy oidc.jwt", "step_finished:deploy vacío"}, e.engine.seen)
	})
	t.Run("se pierden todos los finales", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		e := newE2E(t, step(app.StepStarted, "supply"), step(app.StepStarted, "deploy"))

		require.NoError(t, e.executor.Run(context.Background(), "deploy", "sand"))

		assert.Empty(t, e.leftovers(t), "cerrar la sesión borra el directorio aunque no llegue ningún final")
	})
}

func TestPortal_CtrlCAMitadDeUnPasoDeNubeNoDejaElToken(t *testing.T) {
	defer goleak.VerifyNone(t)
	e := newE2E(t, step(app.StepStarted, "supply"), step(app.CommandFinished, "supply"), step(app.StepFinished, "supply"))
	ctx, cancel := context.WithCancel(context.Background())
	e.engine.onEvent = func(index int) {
		if index == 0 {
			cancel()
		}
	}

	err := e.executor.Run(ctx, "supply", "sand")

	require.ErrorIs(t, err, app.ErrAttemptCanceled)
	assert.Equal(t, []string{"step_started:supply oidc.jwt"}, e.engine.seen)
	assert.Empty(t, e.leftovers(t))
}

func TestPortal_ConexionRevocadaAMitadDetieneLaEjecucion(t *testing.T) {
	defer goleak.VerifyNone(t)
	e := newE2E(t, step(app.StepStarted, "supply"), step(app.CommandFinished, "supply"), step(app.StepFinished, "supply"))
	e.portal.issueErr = fmt.Errorf("%w: %w", app.ErrCloudTokenDenied, app.ErrCloudConnectionInactive)

	err := e.executor.Run(context.Background(), "supply", "sand")

	var lost *app.CloudAccessLostError
	require.ErrorAs(t, err, &lost)
	assert.ErrorContains(t, err, "Ejecuta `vex bootstrap sand`")
	assert.Equal(t, []string{"step_started:supply vacío"}, e.engine.seen, "el motor recibe la cancelación y no sigue")
	assert.Empty(t, e.leftovers(t))
}

func TestPortal_ElBootstrapCambioDesdeQueSeAceptoLaConexion(t *testing.T) {
	e := newE2E(t, step(app.StepStarted, "supply"))
	e.portal.connection.BootstrapSHA256 = strings.Repeat("0", 64)

	err := e.executor.Run(context.Background(), "supply", "sand")

	require.ErrorIs(t, err, app.ErrBootstrapOutdated)
	assert.ErrorContains(t, err, "El bootstrap del pipeline cambió: ejecuta `vex bootstrap sand`")
	assert.Zero(t, e.engine.attempts, "sin crear contenedores")
	assert.Zero(t, e.portal.issued)
	assert.Empty(t, e.leftovers(t))
}

func TestPortal_SinConexionTerminaConLaInstruccion(t *testing.T) {
	e := newE2E(t, step(app.StepStarted, "supply"))
	e.portal.connection = app.CloudConnectionInfo{Status: "absent", Reason: "no_connection", Hint: "vex bootstrap sand"}

	err := e.executor.Run(context.Background(), "supply", "sand")

	assert.EqualError(t, err, "No hay conexión de nube activa para 'sand'. Ejecuta `vex bootstrap sand`")
	assert.Zero(t, e.engine.attempts)
}

func TestCloudErrorHints_VienenDelRegistro(t *testing.T) {
	assert.NotEmpty(t, CloudErrorHints())
	_, err := cloudAdapters().Lookup("azure/v1")
	assert.NoError(t, err)
	_, err = cloudAdapters().Lookup("aws/v1")
	assert.ErrorContains(t, err, "soporta: azure/v1")
}
