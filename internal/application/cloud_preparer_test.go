package application

import (
	"context"
	"errors"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/jairoprogramador/vex/internal/domain/project/aggregates"
	proVos "github.com/jairoprogramador/vex/internal/domain/project/vos"
	"github.com/jairoprogramador/vex/internal/infrastructure/portalclient"
)

// fakeCloudAdapter es un contrato inventado: la aplicación no conoce ninguna nube real.
type fakeCloudAdapter struct {
	contract  ContractID
	bootstrap BootstrapProvider
	trust     TrustProvider
}

func (a fakeCloudAdapter) Contract() ContractID         { return a.contract }
func (a fakeCloudAdapter) Workload() WorkloadContract   { return a }
func (a fakeCloudAdapter) Bootstrap() BootstrapProvider { return a.bootstrap }
func (a fakeCloudAdapter) Trust() TrustProvider         { return a.trust }
func (a fakeCloudAdapter) Env(ConnectionOutputs, string) (map[string]string, []string, error) {
	return map[string]string{}, nil, nil
}
func (a fakeCloudAdapter) ReservedEnvPrefixes() []string { return []string{"NUBE_"} }
func (a fakeCloudAdapter) ExecutionContextKey(params map[string]string) string {
	return params["cuenta"] + "\x00" + params["region"]
}
func (a fakeCloudAdapter) ErrorHints() []ErrorHint {
	return []ErrorHint{{Pattern: regexp.MustCompile("NUBE-401"), Message: "revisa {env}"}}
}

type fakeLogin struct {
	ensures, forgets int
	ensureErr        error
}

func (f *fakeLogin) EnsureToken(context.Context) error { f.ensures++; return f.ensureErr }
func (f *fakeLogin) ForgetToken() error                { f.forgets++; return nil }

type fakeProjectPortal struct {
	responses []portalclient.CreateOrGetProjectResponse
	errs      []error // por llamada; nil cuando se acaban
	requests  []portalclient.CreateOrGetProjectRequest
	synced    []string
}

func (f *fakeProjectPortal) CreateOrGetProject(_ context.Context, req portalclient.CreateOrGetProjectRequest) (portalclient.CreateOrGetProjectResponse, error) {
	call := len(f.requests)
	f.requests = append(f.requests, req)
	if call < len(f.errs) && f.errs[call] != nil {
		return portalclient.CreateOrGetProjectResponse{}, f.errs[call]
	}
	if len(f.responses) == 0 {
		return portalclient.CreateOrGetProjectResponse{PipelineID: "pl-1"}, nil
	}
	return f.responses[min(call, len(f.responses)-1)], nil
}

func (f *fakeProjectPortal) SyncPipeline(_ context.Context, pipelineID string) error {
	f.synced = append(f.synced, pipelineID)
	return nil
}

type fakeCloudConnections struct {
	info  CloudConnectionInfo
	errs  []error // por llamada
	calls []string
}

func (f *fakeCloudConnections) CloudConnection(_ context.Context, projectID, environment string) (CloudConnectionInfo, error) {
	call := len(f.calls)
	f.calls = append(f.calls, projectID+"@"+environment)
	if call < len(f.errs) && f.errs[call] != nil {
		return CloudConnectionInfo{}, f.errs[call]
	}
	return f.info, nil
}

type fakeBootstraps struct {
	bootstrap PipelineBootstrap
	err       error
	reads     []string
}

func (f *fakeBootstraps) Read(pipelineDir, provider string) (PipelineBootstrap, error) {
	f.reads = append(f.reads, pipelineDir+"|"+provider)
	return f.bootstrap, f.err
}

// fakePuller simula docker pull: termina cuando se le deja (release) o cuando se cancela.
type fakePuller struct {
	mu      sync.Mutex
	images  []string
	err     error
	release chan struct{} // nil: termina enseguida
}

func (f *fakePuller) Pull(ctx context.Context, image string) error {
	f.mu.Lock()
	f.images = append(f.images, image)
	release := f.release
	f.mu.Unlock()
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return f.err
}

func (f *fakePuller) pulled() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.images...)
}

const testFingerprint = "be9cd7fb3cf30f9800b778e31b0cc34d3bd9c89907dc158b4079dc7644dcf806"

func activeCloudConnection() CloudConnectionInfo {
	return CloudConnectionInfo{
		Status: ConnectionActive, ConnectionID: "5b0c8a52-3f0e-4d55-9a6e-0f6d0e1c2a11", BootstrapSHA256: testFingerprint,
		Outputs: &ConnectionOutputs{ContractID: "nube/v1", Params: map[string]string{"cuenta": "123", "region": "sur"}},
	}
}

type preparerRig struct {
	login      *fakeLogin
	portal     *fakeProjectPortal
	projects   *stubProjectRepo
	conns      *fakeCloudConnections
	bootstraps *fakeBootstraps
	puller     *fakePuller
	notify     *fakePresenter
	preparer   *CloudPreparer
	project    *aggregates.Project
	// accept es la respuesta de la persona a un cambio de pipeline o de imagen que proponga el portal; proposed,
	// lo que se le enseñó.
	accept   bool
	proposed []ProjectChange
}

func newPreparerRig(t *testing.T, opts ...projectOption) *preparerRig {
	t.Helper()
	project := newProject(t, append([]projectOption{withCloud(t, "", proVos.CredentialsPortal)}, opts...)...)
	r := &preparerRig{
		login: &fakeLogin{}, portal: &fakeProjectPortal{}, projects: &stubProjectRepo{loaded: project, existsBool: true},
		conns: &fakeCloudConnections{info: activeCloudConnection()},
		bootstraps: &fakeBootstraps{bootstrap: PipelineBootstrap{
			Cloud: "nube", Fingerprint: testFingerprint,
			Manifest: BootstrapManifest{Contract: "nube/v1", Template: "main.json", CloudSteps: []string{"supply", "deploy"}},
		}},
		puller: &fakePuller{}, notify: &fakePresenter{}, project: project, accept: true,
	}
	r.preparer = NewCloudPreparer(r.login, r.portal, r.projects, r.conns, r.bootstraps,
		NewCloudAdapters(fakeCloudAdapter{contract: "nube/v1"}), r.puller, r.notify).WithTemplateCatalog(signedCatalog()).
		WithChangeApprover(func(changes []ProjectChange) (bool, error) {
			r.proposed = changes
			return r.accept, nil
		})
	return r
}

// prepare hace las dos fases como las hace el ejecutor.
func (r *preparerRig) prepare(ctx context.Context) (CloudPlan, error) {
	prep, err := r.preparer.Begin(ctx, r.project, nil)
	if err != nil {
		return CloudPlan{}, err
	}
	defer prep.Close()
	return prep.Ready(ctx, Source{Path: "/cache/pipe.git", Commit: "c1"}, "sand")
}

func TestCloudPreparer_ConexionActivaYCompatible(t *testing.T) {
	defer goleak.VerifyNone(t)
	r := newPreparerRig(t)

	plan, err := r.prepare(context.Background())

	require.NoError(t, err)
	assert.True(t, plan.UsesPortal())
	assert.Equal(t, r.conns.info, plan.Connection)
	assert.Equal(t, []string{"supply", "deploy"}, plan.Bootstrap.Manifest.CloudSteps)
	assert.Equal(t, ContractID("nube/v1"), plan.Adapter.Contract())
	assert.Equal(t, 1, r.login.ensures)
	assert.Equal(t, []string{"seed-id@sand"}, r.conns.calls, "una sola consulta de la conexión")
	assert.Equal(t, []string{"/cache/pipe.git|"}, r.bootstraps.reads)
	assert.Equal(t, []string{"local/runtime:v1"}, r.puller.pulled(), "la imagen de registro se baja en segundo plano")
	require.Len(t, r.notify.warnings, 1, "el ref del pipeline es una rama")
	assert.Contains(t, r.notify.warnings[0], `"main"`)
}

func TestCloudPreparer_SinConexionUtilizableDiceQueEjecutar(t *testing.T) {
	tests := []struct {
		status, hint, want string
	}{
		{"absent", "vex bootstrap sand", "No hay conexión de nube activa para 'sand'. Ejecuta `vex bootstrap sand`"},
		{"pending", "", "No hay conexión de nube activa para 'sand'. Ejecuta `vex bootstrap sand`"},
		{"verifying", "", "No hay conexión de nube activa para 'sand'. Ejecuta `vex bootstrap sand`"},
		{"failed", "vex bootstrap sand --reconnect", "no pasó la verificación. Ejecuta `vex bootstrap sand --reconnect`"},
		{"revoked", "", "fue desconectada. Ejecuta `vex bootstrap sand`"},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			r := newPreparerRig(t)
			r.conns.info = CloudConnectionInfo{Status: tt.status, Hint: tt.hint}

			_, err := r.prepare(context.Background())

			var notReady *ConnectionNotReadyError
			require.ErrorAs(t, err, &notReady)
			assert.ErrorContains(t, err, tt.want)
			assert.Len(t, r.conns.calls, 1, "no espera ni vuelve a consultar")
		})
	}
}

func TestCloudPreparer_ElBootstrapDelPipelineCambio(t *testing.T) {
	tests := map[string]func(*preparerRig){
		"otra huella":         func(r *preparerRig) { r.conns.info.BootstrapSHA256 = "0000" },
		"conexión sin huella": func(r *preparerRig) { r.conns.info.BootstrapSHA256 = "" },
		"otro contrato":       func(r *preparerRig) { r.conns.info.Outputs.ContractID = "nube/v2" },
		// Un pipeline propio, que el catálogo firmado no publica: con uno del catálogo, el cambio se detiene antes.
		"cambió el del pipeline": func(r *preparerRig) {
			r.bootstraps.bootstrap.Fingerprint = "ffff"
			r.preparer.WithTemplateCatalog(&fakeCatalog{})
		},
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			r := newPreparerRig(t)
			change(r)

			_, err := r.prepare(context.Background())

			require.ErrorIs(t, err, ErrBootstrapOutdated)
			assert.ErrorContains(t, err, "El bootstrap del pipeline cambió: ejecuta `vex bootstrap sand`")
		})
	}
}

func TestCloudPreparer_LoQueFallaAntesDeConsultarLaConexion(t *testing.T) {
	t.Run("el bootstrap del pipeline no se puede leer", func(t *testing.T) {
		r := newPreparerRig(t, withCloud(t, "aws", proVos.CredentialsPortal))
		r.bootstraps.err = errors.New(`cloud.provider es "aws" pero el pipeline solo trae bootstrap/nube/`)

		_, err := r.prepare(context.Background())

		assert.ErrorContains(t, err, "cloud.provider")
		assert.Equal(t, []string{"/cache/pipe.git|aws"}, r.bootstraps.reads, "se lee la nube que declara el proyecto")
		assert.Empty(t, r.conns.calls)
	})
	t.Run("contrato sin adaptador", func(t *testing.T) {
		r := newPreparerRig(t)
		r.bootstraps.bootstrap.Manifest.Contract = "otra/v3"

		_, err := r.prepare(context.Background())

		var unsupported *UnsupportedContractError
		require.ErrorAs(t, err, &unsupported)
		assert.ErrorContains(t, err, `requiere el contrato de nube "otra/v3"`)
		assert.ErrorContains(t, err, "soporta: nube/v1. Actualiza vex")
		assert.Empty(t, r.conns.calls)
	})
	t.Run("variable con un prefijo reservado del adaptador", func(t *testing.T) {
		r := newPreparerRig(t, withEnv(t, "NUBE_SECRETO", "$X"))

		_, err := r.prepare(context.Background())

		require.ErrorIs(t, err, ErrCloudConfig)
		assert.ErrorContains(t, err, "NUBE_SECRETO")
		assert.Empty(t, r.conns.calls)
	})
}

func TestCloudPreparer_SesionRechazadaPorElPortal(t *testing.T) {
	denied := errors.Join(ErrCloudTokenDenied, ErrPortalSessionInvalid)

	t.Run("al consultar la conexión: inicia sesión otra vez y repite una vez", func(t *testing.T) {
		r := newPreparerRig(t)
		r.conns.errs = []error{denied}

		_, err := r.prepare(context.Background())

		require.NoError(t, err)
		assert.Equal(t, 1, r.login.forgets)
		assert.Equal(t, 2, r.login.ensures)
		assert.Len(t, r.conns.calls, 2)
	})
	t.Run("al consultar la conexión, dos veces: se rinde", func(t *testing.T) {
		r := newPreparerRig(t)
		r.conns.errs = []error{denied, denied}

		_, err := r.prepare(context.Background())

		assert.ErrorContains(t, err, "Ejecuta `vex login`")
		assert.Len(t, r.conns.calls, 2, "un único reintento")
	})
	t.Run("al registrar el proyecto: inicia sesión otra vez y repite una vez", func(t *testing.T) {
		r := newPreparerRig(t)
		r.portal.errs = []error{portalclient.ErrUnauthorized}

		_, err := r.prepare(context.Background())

		require.NoError(t, err)
		assert.Equal(t, 1, r.login.forgets)
		assert.Len(t, r.portal.requests, 2)
	})
	t.Run("al registrar el proyecto, dos veces: se rinde", func(t *testing.T) {
		r := newPreparerRig(t)
		r.portal.errs = []error{portalclient.ErrUnauthorized, portalclient.ErrUnauthorized}

		_, err := r.prepare(context.Background())

		assert.ErrorContains(t, err, "Ejecuta `vex login`")
		assert.Empty(t, r.conns.calls)
	})
	t.Run("sin permiso en el proyecto", func(t *testing.T) {
		r := newPreparerRig(t)
		r.portal.errs = []error{portalclient.ErrForbidden}

		_, err := r.prepare(context.Background())

		assert.ErrorContains(t, err, "no te reconoce como miembro")
		assert.Zero(t, r.login.forgets)
	})
	t.Run("no se pudo iniciar sesión", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		r := newPreparerRig(t)
		r.login.ensureErr = errors.New("device code expired")
		r.puller.release = make(chan struct{}) // la descarga sigue en curso cuando falla el login

		_, err := r.prepare(context.Background())

		assert.ErrorContains(t, err, "iniciar sesión en vex: device code expired")
		assert.Empty(t, r.portal.requests)
	})
}

func TestCloudPreparer_AdoptaLoQueElPortalDiceDelProyecto(t *testing.T) {
	r := newPreparerRig(t)
	r.portal.responses = []portalclient.CreateOrGetProjectResponse{{
		PipelineID: "pl-9", NeedsSync: true,
		Authoritative: &portalclient.AuthoritativeData{
			Project:  portalclient.AuthoritativeProject{RepoURL: "https://github.com/local/app.git", RepoRef: "develop"},
			Pipeline: portalclient.AuthoritativePipeline{URL: "https://github.com/local/pipe.git", Ref: "0123456789abcdef0123456789abcdef01234567", RuntimeImage: "registro/runtime", RuntimeTag: "v2"},
		},
	}}

	_, err := r.prepare(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "0123456789abcdef0123456789abcdef01234567", r.project.Pipeline().Ref(), "lo que se clone después es lo del portal")
	assert.Equal(t, 1, r.projects.saveCalls, "vexconfig.yaml queda guardado")
	assert.True(t, r.projects.saved.Cloud().IsPortal(), "el bloque cloud se conserva")
	assert.Equal(t, []string{"pl-9"}, r.portal.synced)
	assert.Equal(t, []string{"local/runtime:v1", "registro/runtime:v2"}, r.puller.pulled(),
		"si el portal cambia la imagen, se baja la nueva")
	require.Len(t, r.notify.warnings, 1, "avisa del cambio; el ref ya es un commit y no se avisa de él")
	assert.Contains(t, r.notify.warnings[0], "vexconfig.yaml se actualizó")
}

func TestCloudPreparer_LaImagen(t *testing.T) {
	t.Run("una imagen que se construye no se baja", func(t *testing.T) {
		r := newPreparerRig(t, withImage("docker/Dockerfile"))

		_, err := r.prepare(context.Background())

		require.NoError(t, err)
		assert.Empty(t, r.puller.pulled())
	})
	t.Run("Ready espera a que termine la descarga", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		r := newPreparerRig(t)
		r.puller.release = make(chan struct{})
		prep, err := r.preparer.Begin(context.Background(), r.project, nil)
		require.NoError(t, err)
		defer prep.Close()

		ready := make(chan error, 1)
		go func() {
			_, err := prep.Ready(context.Background(), Source{Path: "/cache/pipe.git"}, "sand")
			ready <- err
		}()

		select {
		case <-ready:
			t.Fatal("Ready no debe volver mientras la imagen se descarga")
		case <-time.After(50 * time.Millisecond):
		}
		close(r.puller.release)
		require.NoError(t, <-ready)
	})
	t.Run("la descarga falla", func(t *testing.T) {
		r := newPreparerRig(t)
		r.puller.err = errors.New("docker pull local/runtime:v1: exit status 1: manifest unknown")

		_, err := r.prepare(context.Background())

		assert.ErrorContains(t, err, "no se pudo descargar la imagen de runtime local/runtime:v1")
		assert.ErrorContains(t, err, "manifest unknown")
	})
	t.Run("Ctrl+C durante la descarga la cancela y no deja goroutines", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		r := newPreparerRig(t)
		r.puller.release = make(chan struct{})
		ctx, cancel := context.WithCancel(context.Background())
		prep, err := r.preparer.Begin(ctx, r.project, nil)
		require.NoError(t, err)
		cancel()

		_, err = prep.Ready(ctx, Source{Path: "/cache/pipe.git"}, "sand")
		prep.Close()

		assert.ErrorIs(t, err, context.Canceled)
	})
	t.Run("sin conexión no se espera a la imagen", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		r := newPreparerRig(t)
		r.puller.release = make(chan struct{})
		r.conns.info = CloudConnectionInfo{Status: "absent"}

		_, err := r.prepare(context.Background())

		var notReady *ConnectionNotReadyError
		assert.ErrorAs(t, err, &notReady, "termina en segundos aunque la imagen siga bajando")
	})
}

func TestCloudPreparer_UnRefQueEsUnCommitNoAvisa(t *testing.T) {
	assert.True(t, commitSHAPattern.MatchString("0123456789abcdef0123456789abcdef01234567"))
	for _, mutable := range []string{"main", "v1.2.0", "0123456", "0123456789ABCDEF0123456789abcdef01234567"} {
		assert.False(t, commitSHAPattern.MatchString(mutable), mutable)
	}
}

func TestCloudExecutionContext(t *testing.T) {
	plan := func() CloudPlan {
		return CloudPlan{Connection: activeCloudConnection(), Adapter: fakeCloudAdapter{contract: "nube/v1"}}
	}
	base := CloudExecutionContext(plan())

	assert.Equal(t, base, CloudExecutionContext(plan()), "determinista")
	assert.Regexp(t, `^nube/v1:[0-9a-f]{16}$`, base)
	assert.Regexp(t, `^[A-Za-z0-9._:/@+=-]{1,128}$`, base, "la forma que acepta el motor")
	assert.NotContains(t, base, "5b0c8a52", "no lleva el id de la conexión")
	assert.NotContains(t, base, "123", "ni datos del destino")

	changes := map[string]func(*CloudPlan){
		"otra conexión": func(p *CloudPlan) { p.Connection.ConnectionID = "otra" },
		"otra cuenta":   func(p *CloudPlan) { p.Connection.Outputs.Params["cuenta"] = "456" },
		"otra región":   func(p *CloudPlan) { p.Connection.Outputs.Params["region"] = "norte" },
	}
	for name, change := range changes {
		p := plan()
		change(&p)
		assert.NotEqual(t, base, CloudExecutionContext(p), name)
	}

	same := plan()
	same.Connection.Outputs.Params["otro_dato"] = "x"
	same.Connection.PipelineCommit = "abc"
	assert.Equal(t, base, CloudExecutionContext(same), "lo que no es el destino no cambia el contexto")

	assert.Empty(t, CloudExecutionContext(CloudPlan{}), "sin portal el motor no recibe el campo")
}

// --- el ejecutor con la preparación ---

func newPreparedHarness(t *testing.T, opts ...projectOption) (*harness, *preparerRig, *fakeCredentials) {
	t.Helper()
	r := newPreparerRig(t, opts...)
	h := newHarness(t, r.project)
	r.preparer.notify = h.presenter
	credentials := newFakeCredentials()
	WithPortalCredentials(credentials)(h.executor)
	WithCloudPreparer(r.preparer)(h.executor)
	return h, r, credentials
}

func TestLocalExecutor_ConPortalPreparaAntesDeCrearContenedores(t *testing.T) {
	h, r, credentials := newPreparedHarness(t)

	require.NoError(t, h.executor.Run(context.Background(), "deploy", "sand"))

	require.Len(t, credentials.requests, 1)
	request := credentials.requests[0]
	assert.Equal(t, []string{"supply", "deploy"}, request.CloudSteps)
	require.NotNil(t, request.Connection, "la sesión recibe la conexión ya consultada")
	assert.Equal(t, r.conns.info, *request.Connection)
	assert.Len(t, r.conns.calls, 1)
	assert.Regexp(t, `^nube/v1:[0-9a-f]{16}$`, h.engine.request.Context)
	require.Len(t, h.presenter.clouds, 1)
	assert.Equal(t, "sand", h.presenter.clouds[0].Environment)
	assert.Equal(t, ContractID("nube/v1"), h.presenter.clouds[0].Contract)
	assert.Len(t, h.presenter.clouds[0].Hints, 1)
}

func TestLocalExecutor_SinConexionTerminaSinClonarDeMasNiCrearContenedores(t *testing.T) {
	h, r, credentials := newPreparedHarness(t)
	r.conns.info = CloudConnectionInfo{Status: "absent", Hint: "vex bootstrap sand"}
	guard := &countingInspector{}
	WithPreflight(NewPipelineGuard(guard, NewRecentsService(newMemoryStore())))(h.executor)

	err := h.executor.Run(context.Background(), "deploy", "sand")

	assert.ErrorContains(t, err, "Ejecuta `vex bootstrap sand`")
	assert.Zero(t, h.engine.attempts)
	assert.Zero(t, guard.calls, "ni siquiera el contenedor del pre-vuelo")
	assert.Empty(t, credentials.requests, "no se abre ninguna sesión")
	assert.Empty(t, h.presenter.clouds)
}

// countingInspector cuenta los contenedores que lanzaría el pre-vuelo.
type countingInspector struct{ calls int }

func (c *countingInspector) Check(context.Context, ContainerSpec, CheckRequest) (CheckResult, error) {
	c.calls++
	return CheckResult{Valid: true}, nil
}
func (c *countingInspector) Environments(context.Context, ContainerSpec, PipelineRef) ([]Environment, error) {
	c.calls++
	return nil, nil
}
func (c *countingInspector) Steps(context.Context, ContainerSpec, PipelineRef) ([]PipelineStep, error) {
	c.calls++
	return nil, nil
}

func TestLocalExecutor_SiNoSeAceptaElCambioDelPortalNoSeClonaNada(t *testing.T) {
	// M6: un portal comprometido no puede cambiar qué pipeline se clona y se ejecuta sin que alguien lo acepte.
	h, r, _ := newPreparedHarness(t)
	r.accept = false
	r.portal.responses = []portalclient.CreateOrGetProjectResponse{{
		Authoritative: &portalclient.AuthoritativeData{
			Project:  portalclient.AuthoritativeProject{RepoURL: "https://github.com/local/app.git", RepoRef: "develop"},
			Pipeline: portalclient.AuthoritativePipeline{URL: "https://github.com/atacante/pipe.git", Ref: "main", RuntimeImage: "local/runtime", RuntimeTag: "v1"},
		},
	}}

	err := h.executor.Run(context.Background(), "deploy", "sand")

	require.ErrorIs(t, err, ErrPortalChangeNotAccepted)
	assert.ErrorContains(t, err, "pipeline.url: https://github.com/local/pipe.git → https://github.com/atacante/pipe.git")
	assert.Equal(t, []ProjectChange{{Field: "pipeline.url", Current: "https://github.com/local/pipe.git", Proposed: "https://github.com/atacante/pipe.git"}}, r.proposed)
	assert.Empty(t, h.sources.calls, "no se clona nada")
	assert.Zero(t, r.projects.saveCalls, "vexconfig.yaml no se toca")
}

func TestLocalExecutor_ElPortalCorrigeElPipelineAntesDeClonar(t *testing.T) {
	h, r, _ := newPreparedHarness(t)
	r.portal.responses = []portalclient.CreateOrGetProjectResponse{{
		Authoritative: &portalclient.AuthoritativeData{
			Project:  portalclient.AuthoritativeProject{RepoURL: "https://github.com/local/app.git", RepoRef: "develop"},
			Pipeline: portalclient.AuthoritativePipeline{URL: "https://github.com/local/pipe.git", Ref: "v2", RuntimeImage: "local/runtime", RuntimeTag: "v1"},
		},
	}}

	require.NoError(t, h.executor.Run(context.Background(), "deploy", "sand"))

	assert.Equal(t, []string{"https://github.com/local/app.git@develop", "https://github.com/local/pipe.git@v2"}, h.sources.calls)
}

func TestLocalExecutor_SinPortalNoSeTocaElPortalNiSeEnviaContexto(t *testing.T) {
	for name, opts := range map[string][]projectOption{
		"sin bloque cloud": nil,
		"credentials: env": {withCloud(t, "azure", proVos.CredentialsEnv)},
	} {
		t.Run(name, func(t *testing.T) {
			r := newPreparerRig(t)
			h := newHarness(t, newProject(t, opts...))
			WithCloudPreparer(r.preparer)(h.executor)
			WithPortalCredentials(newFakeCredentials())(h.executor)

			require.NoError(t, h.executor.Run(context.Background(), "test", "sand"))

			assert.Zero(t, r.login.ensures)
			assert.Empty(t, r.portal.requests)
			assert.Empty(t, r.conns.calls)
			assert.Empty(t, r.bootstraps.reads)
			assert.Empty(t, r.puller.pulled())
			assert.Empty(t, h.engine.request.Context)
			assert.Empty(t, h.presenter.clouds)
		})
	}
}

func TestLocalExecutor_GuardaLosTiempos(t *testing.T) {
	h, _, _ := newPreparedHarness(t)
	path := t.TempDir() + "/vex/tiempos.jsonl"
	WithTimingsFile(path)(h.executor)

	require.NoError(t, h.executor.Run(context.Background(), "deploy", "sand"))

	assert.FileExists(t, path)
}

func TestRollback_ConPortalUsaLaConexionDelAmbienteYDeclaraElContexto(t *testing.T) {
	r := newPreparerRig(t)
	h, credentials := newPortalRollbackHarness(t)
	WithRollbackCloudPreparer(r.preparer)(h.service)
	h.knowDeployment(t, "01a113d0-7845-79ee-9381-be4f9efac5ab", "sand", time.Now())
	plan, err := h.service.Plan("efac5ab")
	require.NoError(t, err)

	require.NoError(t, h.service.Run(context.Background(), plan))

	assert.Equal(t, []string{"seed-id@sand"}, r.conns.calls)
	require.Len(t, credentials.requests, 1)
	assert.Equal(t, []string{"supply", "deploy"}, credentials.requests[0].CloudSteps)
	assert.NotNil(t, credentials.requests[0].Connection)
	assert.Regexp(t, `^nube/v1:[0-9a-f]{16}$`, h.engine.request.Context)
	assert.Len(t, h.presenter.clouds, 1)
}

func TestRollback_ConPortalHaceFaltaSaberElAmbiente(t *testing.T) {
	r := newPreparerRig(t)
	h, credentials := newPortalRollbackHarness(t)
	WithRollbackCloudPreparer(r.preparer)(h.service)

	err := h.service.Run(context.Background(), RollbackPlan{DeploymentID: "01a113d0-7845-79ee-9381-be4f9efac5ab"})

	require.ErrorIs(t, err, ErrRollbackEnvironmentUnknown)
	assert.Zero(t, r.login.ensures, "no se toca el portal")
	assert.Empty(t, credentials.requests)
	assert.Zero(t, h.engine.attempts)
}
