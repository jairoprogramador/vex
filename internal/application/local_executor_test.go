package application

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	comVos "github.com/jairoprogramador/vex/internal/domain/common/vos"
	"github.com/jairoprogramador/vex/internal/domain/project/aggregates"
	proVos "github.com/jairoprogramador/vex/internal/domain/project/vos"
)

type fakeSources struct {
	calls []string
	err   error
}

func (f *fakeSources) Ensure(_ context.Context, url, ref string) (Source, error) {
	f.calls = append(f.calls, url+"@"+ref)
	if f.err != nil {
		return Source{}, f.err
	}
	return Source{Path: "/cache/" + filepath.Base(url), Commit: "commit-of-" + filepath.Base(url)}, nil
}

type fakeImages struct {
	builds []ImageBuild
	err    error
}

func (f *fakeImages) Build(_ context.Context, build ImageBuild) error {
	f.builds = append(f.builds, build)
	return f.err
}

type fakeEngine struct {
	result      AttemptResult
	attemptErr  error
	outputs     []CommandOutput
	logsErr     error
	spec        ContainerSpec
	request     AttemptRequest
	attempts    int
	logRequests []LogsRequest
}

func (f *fakeEngine) Attempt(_ context.Context, spec ContainerSpec, req AttemptRequest, onEvent func(EngineEvent)) (AttemptResult, error) {
	f.attempts++
	f.spec, f.request = spec, req
	onEvent(EngineEvent{Kind: AttemptStarted, AttemptID: "i1"})
	return f.result, f.attemptErr
}

func (f *fakeEngine) Logs(_ context.Context, _ ContainerSpec, req LogsRequest) ([]CommandOutput, error) {
	f.logRequests = append(f.logRequests, req)
	return f.outputs, f.logsErr
}

type fakePresenter struct {
	infos    []string
	warnings []string
	events   []EngineEvent
	results  []AttemptResult
	failed   []CommandOutput
}

func (f *fakePresenter) Info(m string)                    { f.infos = append(f.infos, m) }
func (f *fakePresenter) Warn(m string)                    { f.warnings = append(f.warnings, m) }
func (f *fakePresenter) Event(e EngineEvent)              { f.events = append(f.events, e) }
func (f *fakePresenter) Result(r AttemptResult)           { f.results = append(f.results, r) }
func (f *fakePresenter) FailedCommands(o []CommandOutput) { f.failed = o }

type projectOption func(*projectParts)

type projectParts struct {
	image   string
	volumes []proVos.Volume
	envs    []proVos.EnvVar
	args    []proVos.Argument
}

func withImage(spec string) projectOption { return func(p *projectParts) { p.image = spec } }
func withEnv(t *testing.T, name, value string) projectOption {
	env, err := proVos.NewEnvVar(name, value)
	require.NoError(t, err)
	return func(p *projectParts) { p.envs = append(p.envs, env) }
}
func withVolume(t *testing.T, host, container string) projectOption {
	volume, err := proVos.NewVolume(host, container)
	require.NoError(t, err)
	return func(p *projectParts) { p.volumes = append(p.volumes, volume) }
}
func withRuntimeArg(t *testing.T, name, value string) projectOption {
	arg, err := proVos.NewArgument(name, value)
	require.NoError(t, err)
	return func(p *projectParts) { p.args = append(p.args, arg) }
}

func newProject(t *testing.T, opts ...projectOption) *aggregates.Project {
	t.Helper()
	parts := projectParts{image: "local/runtime:v1"}
	for _, opt := range opts {
		opt(&parts)
	}

	id, err := proVos.NewProjectID("seed-id")
	require.NoError(t, err)
	data, err := proVos.NewProjectData("Demo", "vex", "shikigami", "desc", "https://github.com/local/app.git", "develop")
	require.NoError(t, err)
	pipeline, err := comVos.NewPipeline("https://github.com/local/pipe.git", "main")
	require.NoError(t, err)
	image, err := comVos.NewImage(parts.image)
	require.NoError(t, err)

	runtime := proVos.NewRuntime(
		proVos.WithImage(image), proVos.WithVolumes(parts.volumes), proVos.WithEnv(parts.envs), proVos.WithArgs(parts.args))
	project, err := aggregates.NewProject(id, data, pipeline, runtime)
	require.NoError(t, err)
	return project
}

type harness struct {
	executor  *LocalExecutorService
	sources   *fakeSources
	images    *fakeImages
	engine    *fakeEngine
	presenter *fakePresenter
	config    LocalExecutorConfig
}

func newHarness(t *testing.T, project *aggregates.Project) *harness {
	t.Helper()
	base := t.TempDir()
	h := &harness{
		sources:   &fakeSources{},
		images:    &fakeImages{},
		engine:    &fakeEngine{result: AttemptResult{AttemptID: "i1", Status: AttemptSucceeded}},
		presenter: &fakePresenter{},
		config: LocalExecutorConfig{
			WorkDir:     "/work",
			StoreDir:    filepath.Join(base, "store"),
			SpaceDir:    filepath.Join(base, "space"),
			MaterialDir: filepath.Join(base, "material"),
			Requester:   "jailux",
		},
	}
	h.executor = NewLocalExecutorService(
		&stubProjectRepo{loaded: project, existsBool: project != nil}, h.sources, h.images, h.engine, h.presenter, h.config)
	return h
}

func TestLocalExecutor_ClonaProyectoYPipelineYArmaLaPeticion(t *testing.T) {
	h := newHarness(t, newProject(t))

	require.NoError(t, h.executor.Run(context.Background(), "test", "sand"))

	assert.Equal(t,
		[]string{"https://github.com/local/app.git@develop", "https://github.com/local/pipe.git@main"},
		h.sources.calls)
	assert.Equal(t, AttemptRequest{
		Environment:    "sand",
		Requester:      "jailux",
		ProjectSource:  "/proyecto",
		ProjectCommit:  "commit-of-app.git",
		PipelineSource: "/pipeline",
		PipelineCommit: "commit-of-pipe.git",
		UntilStep:      "test",
		Project:        ProjectMetadata{ID: "seed-id", Name: "Demo", Organization: "vex", Team: "shikigami"},
	}, h.engine.request)
	assert.Empty(t, h.images.builds, "con tag explícito no hay build")
	assert.Equal(t, "local/runtime:v1", h.engine.spec.Image)
	assert.Equal(t, []AttemptResult{h.engine.result}, h.presenter.results)
	assert.Equal(t, []EngineEvent{{Kind: AttemptStarted, AttemptID: "i1"}}, h.presenter.events)
}

func TestLocalExecutor_MontajesYVariablesDelMotor(t *testing.T) {
	h := newHarness(t, newProject(t, withVolume(t, "/home/u/.m2", "/home/vex/.m2")))

	require.NoError(t, h.executor.Run(context.Background(), "test", "sand"))

	assert.Equal(t, []Mount{
		{Source: "/cache/app.git", Target: "/proyecto", ReadOnly: true},
		{Source: "/cache/pipe.git", Target: "/pipeline", ReadOnly: true},
		{Source: h.config.StoreDir, Target: "/vex/almacen"},
		{Source: h.config.SpaceDir, Target: "/vex/espacio"},
		{Source: h.config.MaterialDir, Target: "/vex/material"},
		{Source: "/home/u/.m2", Target: "/home/vex/.m2"},
	}, h.engine.spec.Mounts, "los volúmenes del usuario van al final y el orden es fijo")
	assert.Equal(t, []EnvVar{
		{Name: "VEX_ALMACEN", Value: "/vex/almacen"},
		{Name: "VEX_ESPACIO", Value: "/vex/espacio"},
		{Name: "VEX_MATERIAL", Value: "/vex/material"},
	}, h.engine.spec.Env)
}

func TestLocalExecutor_CreaLosDirectoriosQueSeMontan(t *testing.T) {
	h := newHarness(t, newProject(t))

	require.NoError(t, h.executor.Run(context.Background(), "test", "sand"))

	for _, dir := range []string{h.config.StoreDir, h.config.SpaceDir, h.config.MaterialDir} {
		assert.DirExists(t, dir)
	}
}

func TestLocalExecutor_ExpandeLasVariablesDelHostComoSecretos(t *testing.T) {
	t.Setenv("ARM_SECRET", "s3cr3t")
	t.Setenv("VEX_TEST_USER", "ana")
	t.Setenv("VEX_TEST_UNSET", "")
	h := newHarness(t, newProject(t,
		withEnv(t, "ARM_CLIENT_SECRET", "$ARM_SECRET"),
		withEnv(t, "WHO", "user-${VEX_TEST_USER}"),
		withEnv(t, "LITERAL", "tal-cual"),
		withEnv(t, "MISSING", "$VEX_TEST_UNSET"),
	))

	require.NoError(t, h.executor.Run(context.Background(), "test", "sand"))

	assert.Equal(t, map[string]string{
		"ARM_CLIENT_SECRET": "s3cr3t",
		"WHO":               "user-ana",
		"LITERAL":           "tal-cual",
	}, h.engine.request.Secrets)
	require.Len(t, h.presenter.warnings, 1)
	assert.Contains(t, h.presenter.warnings[0], "MISSING")
	for _, mount := range h.engine.spec.Mounts {
		assert.NotContains(t, mount.Source+mount.Target, "s3cr3t")
	}
	for _, env := range h.engine.spec.Env {
		assert.NotContains(t, env.Value, "s3cr3t", "los secretos no van como env del contenedor")
	}
}

func TestLocalExecutor_SinEnvsNoHaySecretos(t *testing.T) {
	h := newHarness(t, newProject(t))

	require.NoError(t, h.executor.Run(context.Background(), "test", "sand"))

	assert.Nil(t, h.engine.request.Secrets)
}

func TestLocalExecutor_ConstruyeLaImagenCuandoRuntimeApuntaAUnDockerfile(t *testing.T) {
	h := newHarness(t, newProject(t,
		withImage("docker/MyDockerfile"),
		withRuntimeArg(t, "MAVEN_VERSION", "3.9.12"),
		withRuntimeArg(t, "DEV_UID", "4242"),
	))

	require.NoError(t, h.executor.Run(context.Background(), "test", "sand"))

	require.Len(t, h.images.builds, 1)
	build := h.images.builds[0]
	assert.Equal(t, "demoseed-i:latest", build.Tag, "nombre en minúsculas + 6 caracteres del id")
	assert.Equal(t, "docker/MyDockerfile", build.Dockerfile)
	assert.Equal(t, "/work", build.ContextDir)
	assert.Equal(t, build.Tag, h.engine.spec.Image)

	args := map[string]string{}
	for _, arg := range build.Args {
		_, duplicated := args[arg.Name]
		assert.False(t, duplicated, "%s repetido", arg.Name)
		args[arg.Name] = arg.Value
	}
	assert.Equal(t, "3.9.12", args["MAVEN_VERSION"])
	assert.Equal(t, "4242", args["DEV_UID"], "los args del proyecto prevalecen sobre el uid del host")
	if os.Getuid() >= 0 {
		assert.Contains(t, args, "DEV_GID")
	}
}

func TestLocalExecutor_UnIntentoFallidoMuestraLaSalidaDeLosComandosFallidos(t *testing.T) {
	h := newHarness(t, newProject(t))
	h.engine.result = AttemptResult{AttemptID: "i1", Status: AttemptFailed}
	h.engine.outputs = []CommandOutput{{Step: "test", Command: "mvn test", Text: "boom"}}

	err := h.executor.Run(context.Background(), "test", "sand")

	assert.ErrorIs(t, err, ErrAttemptFailed)
	assert.Equal(t, []LogsRequest{{AttemptID: "i1", OnlyFailed: true}}, h.engine.logRequests)
	assert.Equal(t, h.engine.outputs, h.presenter.failed)
	assert.Len(t, h.presenter.results, 1, "el resumen se muestra antes de la salida fallida")
}

func TestLocalExecutor_SiFallanLosLogsElFalloDelIntentoSigueSiendoElError(t *testing.T) {
	h := newHarness(t, newProject(t))
	h.engine.result = AttemptResult{AttemptID: "i1", Status: AttemptFailed}
	h.engine.logsErr = errors.New("docker murió")

	err := h.executor.Run(context.Background(), "test", "sand")

	assert.ErrorIs(t, err, ErrAttemptFailed)
	assert.Nil(t, h.presenter.failed)
	require.Len(t, h.presenter.warnings, 1)
	assert.Contains(t, h.presenter.warnings[0], "docker murió")
}

func TestLocalExecutor_SinAmbienteNoHaceNadaNiLlamaAlMotor(t *testing.T) {
	h := newHarness(t, newProject(t))

	err := h.executor.Run(context.Background(), "test", "")

	assert.ErrorIs(t, err, ErrEnvironmentRequired)
	assert.Empty(t, h.sources.calls, "no clona ni construye antes de validar")
	assert.Zero(t, h.engine.attempts)
}

func TestLocalExecutor_CancelarDuranteElClonadoEsUnaCancelacion(t *testing.T) {
	h := newHarness(t, newProject(t))
	ctx, cancel := context.WithCancel(context.Background())
	h.sources.err = errors.New("signal: killed")
	cancel()

	err := h.executor.Run(ctx, "test", "sand")

	assert.ErrorIs(t, err, ErrAttemptCanceled)
	assert.Zero(t, h.engine.attempts)
}

func TestLocalExecutor_UnIntentoCanceladoNoPideLogs(t *testing.T) {
	h := newHarness(t, newProject(t))
	h.engine.result = AttemptResult{AttemptID: "i1", Status: AttemptCanceled}

	err := h.executor.Run(context.Background(), "test", "sand")

	assert.ErrorIs(t, err, ErrAttemptCanceled)
	assert.Empty(t, h.engine.logRequests)
}

func TestLocalExecutor_ErroresDelMotor(t *testing.T) {
	t.Run("cancelado antes de abrir el intento es una cancelación", func(t *testing.T) {
		h := newHarness(t, newProject(t))
		h.engine.attemptErr = &EngineError{Kind: EngineCanceled}

		err := h.executor.Run(context.Background(), "test", "sand")

		assert.ErrorIs(t, err, ErrAttemptCanceled)
	})
	t.Run("los demás errores se propagan tal cual", func(t *testing.T) {
		h := newHarness(t, newProject(t))
		busy := &EngineError{Kind: EngineEnvironmentBusy, Environment: "sand", AttemptID: "i9"}
		h.engine.attemptErr = busy

		err := h.executor.Run(context.Background(), "test", "sand")

		var got *EngineError
		require.ErrorAs(t, err, &got)
		assert.Equal(t, EngineEnvironmentBusy, got.Kind)
		assert.Empty(t, h.presenter.results, "sin resultado no hay resumen")
	})
	t.Run("completa el ambiente pedido cuando el error no lo trae", func(t *testing.T) {
		h := newHarness(t, newProject(t))
		h.engine.attemptErr = &EngineError{Kind: EnginePipelineRejected, Variable: "etiqueta"}

		err := h.executor.Run(context.Background(), "test", "nope")

		var got *EngineError
		require.ErrorAs(t, err, &got)
		assert.Equal(t, "nope", got.Environment)
	})
}

func TestLocalExecutor_ProyectoNoInicializado(t *testing.T) {
	h := newHarness(t, nil)

	err := h.executor.Run(context.Background(), "test", "sand")

	assert.ErrorContains(t, err, MessageProjectNotInitialized)
	assert.Zero(t, h.engine.attempts)
	assert.Empty(t, h.sources.calls)
}

func TestLocalExecutor_SiFallaElClonadoNoSeLlamaAlMotor(t *testing.T) {
	h := newHarness(t, newProject(t))
	h.sources.err = errors.New("sin acceso")

	err := h.executor.Run(context.Background(), "test", "sand")

	assert.ErrorContains(t, err, "preparar proyecto")
	assert.ErrorContains(t, err, "sin acceso")
	assert.Zero(t, h.engine.attempts)
	assert.Empty(t, h.images.builds)
}

func TestLocalExecutor_SiFallaElBuildNoSeLlamaAlMotor(t *testing.T) {
	h := newHarness(t, newProject(t, withImage("Dockerfile")))
	h.images.err = errors.New("build roto")

	err := h.executor.Run(context.Background(), "test", "sand")

	assert.ErrorContains(t, err, "construir imagen")
	assert.Zero(t, h.engine.attempts)
}
