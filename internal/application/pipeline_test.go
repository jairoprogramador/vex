package application

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type pipelineHarness struct {
	service *PipelineService
	engine  *specRecordingPipelineEngine
	sources *fakeSources
	recents *RecentsService
}

// specRecordingPipelineEngine es el motor falso del guardián, que además recuerda el contenedor con que se le llamó.
type specRecordingPipelineEngine struct {
	fakePipelineEngine
	specs []ContainerSpec
}

func (f *specRecordingPipelineEngine) Environments(ctx context.Context, spec ContainerSpec, p PipelineRef) ([]Environment, error) {
	f.specs = append(f.specs, spec)
	return f.fakePipelineEngine.Environments(ctx, spec, p)
}

func (f *specRecordingPipelineEngine) Steps(ctx context.Context, spec ContainerSpec, p PipelineRef) ([]PipelineStep, error) {
	f.specs = append(f.specs, spec)
	return f.fakePipelineEngine.Steps(ctx, spec, p)
}

func (f *specRecordingPipelineEngine) Check(ctx context.Context, spec ContainerSpec, r CheckRequest) (CheckResult, error) {
	f.specs = append(f.specs, spec)
	return f.fakePipelineEngine.Check(ctx, spec, r)
}

func newPipelineHarness(t *testing.T) *pipelineHarness {
	t.Helper()
	base := t.TempDir()
	sources := &fakeSources{}
	workspace := NewWorkspace(
		&stubProjectRepo{loaded: newProject(t), existsBool: true}, sources, &fakeImages{}, &fakePresenter{},
		LocalExecutorConfig{
			WorkDir: "/work", StoreDir: filepath.Join(base, "store"), SpaceDir: filepath.Join(base, "space"),
			MaterialDir: filepath.Join(base, "material"), Requester: "jailux",
		})
	engine := &specRecordingPipelineEngine{fakePipelineEngine: fakePipelineEngine{
		result:       CheckResult{Valid: true},
		environments: []Environment{{Value: "sand", Name: "sandbox"}, {Value: "prod", Name: "production", Protected: true}},
		steps:        []PipelineStep{{Name: "test", Order: 1}, {Name: "deploy", Order: 2}},
	}}
	recents := NewRecentsService(newMemoryStore())
	return &pipelineHarness{
		service: NewPipelineService(workspace, engine, NewPipelineGuard(engine, recents), recents),
		engine:  engine, sources: sources, recents: recents,
	}
}

func TestPipeline_ClonaSoloElPipelineYLoMontaSinElProyecto(t *testing.T) {
	h := newPipelineHarness(t)

	_, err := h.service.Environments(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []string{"https://github.com/local/pipe.git@main"}, h.sources.calls, "el proyecto no se clona")
	require.Len(t, h.engine.specs, 1)
	targets := []string{h.engine.specs[0].Mounts[0].Target, h.engine.specs[0].Mounts[1].Target}
	assert.Equal(t, []string{"/pipeline", "/vex/almacen"}, targets)
	assert.True(t, h.engine.specs[0].Mounts[0].ReadOnly)
}

func TestPipeline_LosAmbientesSeRecuerdanConSuProteccion(t *testing.T) {
	h := newPipelineHarness(t)

	environments, err := h.service.Environments(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []Environment{{Value: "sand", Name: "sandbox"}, {Value: "prod", Name: "production", Protected: true}}, environments)
	catalog, ok, _ := h.recents.Catalog(projectKey)
	require.True(t, ok)
	assert.Equal(t, environments, catalog.Environments)
}

func TestPipeline_LosPasosSeRecuerdanSinPisarLosAmbientes(t *testing.T) {
	h := newPipelineHarness(t)
	_, err := h.service.Environments(context.Background())
	require.NoError(t, err)

	steps, err := h.service.Steps(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []PipelineStep{{Name: "test", Order: 1}, {Name: "deploy", Order: 2}}, steps)
	catalog, _, _ := h.recents.Catalog(projectKey)
	assert.Len(t, catalog.Environments, 2)
	assert.Len(t, catalog.Steps, 2)
}

func TestPipeline_CheckEnviaElPipelineConSuCommitYLosMetadatos(t *testing.T) {
	h := newPipelineHarness(t)
	var sent CheckRequest
	h.engine.fakePipelineEngine.result = CheckResult{Valid: true}
	wrapped := &captureCheck{specRecordingPipelineEngine: h.engine, sent: &sent}
	h.service.guard = NewPipelineGuard(wrapped, h.recents)

	err := h.service.Check(context.Background(), "deploy", "sand")

	require.NoError(t, err)
	assert.Equal(t, CheckRequest{
		Environment: "sand", Requester: "jailux", UntilStep: "deploy",
		Pipeline: PipelineRef{Source: "/pipeline", Commit: "commit-of-pipe.git"},
		Project:  ProjectMetadata{ID: "seed-id", Name: "Demo", Organization: "vex", Team: "shikigami"},
	}, sent)
}

type captureCheck struct {
	*specRecordingPipelineEngine
	sent *CheckRequest
}

func (c *captureCheck) Check(ctx context.Context, spec ContainerSpec, req CheckRequest) (CheckResult, error) {
	*c.sent = req
	return c.specRecordingPipelineEngine.Check(ctx, spec, req)
}

func TestPipeline_CheckSinAmbienteNoHaceNada(t *testing.T) {
	h := newPipelineHarness(t)

	err := h.service.Check(context.Background(), "deploy", "")

	assert.ErrorIs(t, err, ErrEnvironmentRequired)
	assert.Empty(t, h.sources.calls, "ni siquiera clona")
}

func TestPipeline_CheckDevuelveLoQueEncuentraElGuardian(t *testing.T) {
	h := newPipelineHarness(t)
	h.engine.fakePipelineEngine.result = CheckResult{Missing: []MissingVariables{{Step: "deploy", Variables: []string{"db_url"}}}}

	err := h.service.Check(context.Background(), "deploy", "sand")

	var failed *CheckFailedError
	require.ErrorAs(t, err, &failed)
	assert.Equal(t, "db_url", failed.Missing[0].Variables[0])
}

func TestPipeline_ProyectoNoInicializado(t *testing.T) {
	h := newPipelineHarness(t)
	h.service.workspace.projects = &stubProjectRepo{existsBool: false}

	_, err := h.service.Steps(context.Background())

	assert.ErrorContains(t, err, MessageProjectNotInitialized)
}

// --- pre-vuelo del ejecutor ---

func newPreflightHarness(t *testing.T, engineResult CheckResult, engineErr error) (*harness, *fakePipelineEngine) {
	t.Helper()
	h := newHarness(t, newProject(t))
	checker := &fakePipelineEngine{
		result: engineResult, checkErr: engineErr,
		environments: []Environment{{Value: "sand"}, {Value: "prod"}},
		steps:        []PipelineStep{{Name: "test"}, {Name: "deploy"}},
	}
	guard := NewPipelineGuard(checker, NewRecentsService(newMemoryStore()))
	h.executor = NewLocalExecutorService(
		NewWorkspace(&stubProjectRepo{loaded: newProject(t), existsBool: true}, h.sources, h.images, h.presenter, h.config),
		h.engine, h.presenter, WithPreflight(guard))
	return h, checker
}

func TestPreflight_UnPipelineListoEjecutaElIntento(t *testing.T) {
	h, checker := newPreflightHarness(t, CheckResult{Valid: true}, nil)

	err := h.executor.Run(context.Background(), "test", "sand")

	require.NoError(t, err)
	assert.Equal(t, []string{"Check:sand:test"}, checker.calls)
	assert.Equal(t, 1, h.engine.attempts)
}

func TestPreflight_SiFallaNoSeAbreNingunIntento(t *testing.T) {
	h, _ := newPreflightHarness(t, CheckResult{Missing: []MissingVariables{{Step: "test", Variables: []string{"x"}}}}, nil)

	err := h.executor.Run(context.Background(), "test", "sand")

	var failed *CheckFailedError
	require.ErrorAs(t, err, &failed)
	assert.Zero(t, h.engine.attempts, "sin intento no queda un fallido inútil en el historial")
	assert.Empty(t, h.presenter.results)
}

func TestPreflight_UnAmbienteMalTecleadoSeExplicaSinAbrirIntento(t *testing.T) {
	h, _ := newPreflightHarness(t, CheckResult{}, invalidParams("Ambiente", "sandd"))

	err := h.executor.Run(context.Background(), "test", "sandd")

	var unknown *UnknownEnvironmentError
	require.ErrorAs(t, err, &unknown)
	assert.Equal(t, "sand", unknown.Suggestion)
	assert.Zero(t, h.engine.attempts)
}

func TestPreflight_SinGuardianNoSeComprueba(t *testing.T) {
	h := newHarness(t, newProject(t))

	require.NoError(t, h.executor.Run(context.Background(), "test", "sand"))

	assert.Equal(t, 1, h.engine.attempts, "--no-check: va directo al intento")
}

// --- explicador de errores del ejecutor (sin pre-vuelo) ---

func newExplainerHarness(t *testing.T, attemptErr error) (*harness, *fakePipelineEngine) {
	t.Helper()
	h := newHarness(t, newProject(t))
	h.engine.attemptErr = attemptErr
	catalog := &fakePipelineEngine{
		environments: []Environment{{Value: "sand"}, {Value: "stag"}, {Value: "prod"}},
		steps:        []PipelineStep{{Name: "test"}, {Name: "package"}, {Name: "deploy"}},
	}
	guard := NewPipelineGuard(catalog, NewRecentsService(newMemoryStore()))
	h.executor = NewLocalExecutorService(
		NewWorkspace(&stubProjectRepo{loaded: newProject(t), existsBool: true}, h.sources, h.images, h.presenter, h.config),
		h.engine, h.presenter, WithErrorExplainer(guard))
	return h, catalog
}

func TestExplainer_UnAmbienteRechazadoPorElMotorSeExplicaSinPreVuelo(t *testing.T) {
	h, catalog := newExplainerHarness(t, invalidParams("Ambiente", "sandd"))

	err := h.executor.Run(context.Background(), "test", "sandd")

	var unknown *UnknownEnvironmentError
	require.ErrorAs(t, err, &unknown)
	assert.Equal(t, "sand", unknown.Suggestion)
	assert.Equal(t, []string{"Environments"}, catalog.calls, "sin pre-vuelo no hay Check: solo se pregunta el catálogo al fallar")
}

func TestExplainer_UnPasoRechazadoPorElMotorSeExplica(t *testing.T) {
	h, _ := newExplainerHarness(t, invalidParams("HastaPaso", "pakage"))

	err := h.executor.Run(context.Background(), "pakage", "sand")

	var unknown *UnknownStepError
	require.ErrorAs(t, err, &unknown)
	assert.Equal(t, "package", unknown.Suggestion)
}

func TestExplainer_EnElCaminoFelizNoPreguntaNada(t *testing.T) {
	h, catalog := newExplainerHarness(t, nil)

	require.NoError(t, h.executor.Run(context.Background(), "test", "sand"))

	assert.Empty(t, catalog.calls, "ni Check ni catálogo: cero coste")
}

func TestExplainer_OtrosErroresDelMotorNoSeTocan(t *testing.T) {
	busy := &EngineError{Kind: EngineEnvironmentBusy, Environment: "sand", AttemptID: "i9"}
	h, catalog := newExplainerHarness(t, busy)

	err := h.executor.Run(context.Background(), "test", "sand")

	var got *EngineError
	require.ErrorAs(t, err, &got)
	assert.Equal(t, EngineEnvironmentBusy, got.Kind)
	assert.Empty(t, catalog.calls)
}
