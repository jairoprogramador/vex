package application

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRollbackEngine struct {
	result  AttemptResult
	err     error
	outputs []CommandOutput

	request  RollbackRequest
	spec     ContainerSpec
	attempts int
	logsReq  LogsRequest
}

func (f *fakeRollbackEngine) Rollback(_ context.Context, spec ContainerSpec, req RollbackRequest, onEvent func(EngineEvent)) (AttemptResult, error) {
	f.attempts++
	f.spec, f.request = spec, req
	onEvent(EngineEvent{Kind: AttemptStarted, AttemptID: "r1"})
	return f.result, f.err
}

func (f *fakeRollbackEngine) Logs(_ context.Context, _ ContainerSpec, req LogsRequest) ([]CommandOutput, error) {
	f.logsReq = req
	return f.outputs, nil
}

type rollbackHarness struct {
	service   *RollbackService
	engine    *fakeRollbackEngine
	sources   *fakeSources
	presenter *fakePresenter
	recents   *RecentsService
}

func newRollbackHarness(t *testing.T, opts ...projectOption) *rollbackHarness {
	t.Helper()
	base := t.TempDir()
	sources, presenter := &fakeSources{}, &fakePresenter{}
	workspace := NewWorkspace(
		&stubProjectRepo{loaded: newProject(t, opts...), existsBool: true}, sources, &fakeImages{}, presenter,
		LocalExecutorConfig{
			WorkDir: "/work", StoreDir: filepath.Join(base, "store"), SpaceDir: filepath.Join(base, "space"),
			MaterialDir: filepath.Join(base, "material"), Requester: "jailux",
		})
	engine := &fakeRollbackEngine{result: AttemptResult{AttemptID: "r1", Status: AttemptSucceeded, DeploymentID: "d-new"}}
	recents := NewRecentsService(newMemoryStore())
	return &rollbackHarness{
		service: NewRollbackService(workspace, engine, presenter, recents), engine: engine,
		sources: sources, presenter: presenter, recents: recents,
	}
}

func (h *rollbackHarness) knowDeployment(t *testing.T, id, environment string, at time.Time) {
	t.Helper()
	require.NoError(t, h.recents.RememberDeployments(projectKey, []Deployment{{ID: id, Environment: environment, At: at}}))
}

func TestRollback_PlanResuelveElIdYLoQueSabeDelDespliegue(t *testing.T) {
	h := newRollbackHarness(t)
	at := time.Now().Add(-48 * time.Hour)
	h.knowDeployment(t, idB, "prod", at)

	plan, err := h.service.Plan("f974d0dee")

	require.NoError(t, err)
	assert.Equal(t, RollbackPlan{DeploymentID: idB, Environment: "prod", DeployedAt: at}, plan)
	assert.Zero(t, h.engine.attempts, "planificar no ejecuta nada")
	assert.Empty(t, h.sources.calls, "ni clona")
}

func TestRollback_PlanConUnIdCompletoQueElCLINoHaVistoSigueAdelante(t *testing.T) {
	h := newRollbackHarness(t)

	plan, err := h.service.Plan(idC)

	require.NoError(t, err)
	assert.Equal(t, RollbackPlan{DeploymentID: idC}, plan, "el ambiente y la fecha los sabe el motor, no hace falta bloquear")
}

func TestRollback_PlanErrores(t *testing.T) {
	h := newRollbackHarness(t)
	h.knowDeployment(t, idB, "prod", time.Now())

	_, errEmpty := h.service.Plan("")
	_, errShort := h.service.Plan("abc")
	_, errUnknown := h.service.Plan("zzzzzzz")

	assert.ErrorIs(t, errEmpty, ErrDeploymentRequired)
	assert.ErrorIs(t, errShort, ErrIDTooShort)
	assert.ErrorIs(t, errUnknown, ErrUnknownID)
}

func TestRollback_PlanFallaAntesDePreguntarSiElProyectoNoEstaInicializado(t *testing.T) {
	h := newRollbackHarness(t)
	h.service.workspace.projects = &stubProjectRepo{existsBool: false}

	_, err := h.service.Plan(idB)

	assert.ErrorContains(t, err, MessageProjectNotInitialized)
}

func TestRollback_RunClonaMontaTodoYPideElRollbackConLasCredenciales(t *testing.T) {
	t.Setenv("ARM_SECRET", "s3cr3t")
	h := newRollbackHarness(t, withEnv(t, "ARM_CLIENT_SECRET", "$ARM_SECRET"))
	h.knowDeployment(t, idB, "prod", time.Now())

	err := h.service.Run(context.Background(), RollbackPlan{DeploymentID: idB, Environment: "prod"})

	require.NoError(t, err)
	assert.Equal(t, []string{"https://github.com/local/app.git@develop", "https://github.com/local/pipe.git@main"}, h.sources.calls)
	assert.Equal(t, RollbackRequest{
		DeploymentID: idB, Requester: "jailux",
		Project: ProjectMetadata{ID: "seed-id", Name: "Demo", Organization: "vex", Team: "shikigami"},
		Secrets: map[string]string{"ARM_CLIENT_SECRET": "s3cr3t"},
	}, h.engine.request)

	targets := make([]string, len(h.engine.spec.Mounts))
	for i, m := range h.engine.spec.Mounts {
		targets[i] = m.Target
	}
	assert.Equal(t, []string{"/proyecto", "/pipeline", "/vex/almacen", "/vex/espacio", "/vex/material"}, targets,
		"las mismas rutas de siempre: el motor las guardó en el despliegue y las vuelve a abrir")
}

func TestRollback_RunMuestraElAvanceYElResultado(t *testing.T) {
	h := newRollbackHarness(t)

	require.NoError(t, h.service.Run(context.Background(), RollbackPlan{DeploymentID: idB, Environment: "prod"}))

	assert.Equal(t, []EngineEvent{{Kind: AttemptStarted, AttemptID: "r1"}}, h.presenter.events)
	assert.Equal(t, []AttemptResult{h.engine.result}, h.presenter.results)
}

func TestRollback_RunRecuerdaElIntentoYElDespliegueNuevo(t *testing.T) {
	h := newRollbackHarness(t)

	require.NoError(t, h.service.Run(context.Background(), RollbackPlan{DeploymentID: idB, Environment: "prod"}))

	last, ok, _ := h.recents.LastAttempt(projectKey, false)
	require.True(t, ok)
	assert.Equal(t, "r1", last.ID)
	assert.Equal(t, "prod", last.Environment)
	assert.Equal(t, AttemptSucceeded, last.Status)
	deployment, ok, _ := h.recents.FindDeployment(projectKey, "d-new")
	require.True(t, ok, "se puede lanzar enseguida con `vex release prod <id corto>`")
	assert.Equal(t, "prod", deployment.Environment)
}

func TestRollback_SinSaberElAmbienteNoInventaUnDespliegueRecordado(t *testing.T) {
	h := newRollbackHarness(t)

	require.NoError(t, h.service.Run(context.Background(), RollbackPlan{DeploymentID: idB}))

	_, ok, _ := h.recents.FindDeployment(projectKey, "d-new")
	assert.False(t, ok)
}

func TestRollback_UnRollbackFallidoMuestraLaSalidaYOrienta(t *testing.T) {
	h := newRollbackHarness(t)
	h.engine.result = AttemptResult{AttemptID: "r1", Status: AttemptFailed}
	h.engine.outputs = []CommandOutput{{Step: "deploy", Command: "terraform apply", Text: "boom"}}

	err := h.service.Run(context.Background(), RollbackPlan{DeploymentID: idB, Environment: "prod"})

	assert.ErrorIs(t, err, ErrAttemptFailed)
	assert.Equal(t, LogsRequest{AttemptID: "r1", OnlyFailed: true}, h.engine.logsReq)
	assert.Equal(t, "r1", h.presenter.failedID)
	assert.Equal(t, h.engine.outputs, h.presenter.failed)
}

func TestRollback_Cancelaciones(t *testing.T) {
	t.Run("un rollback cancelado es un resultado", func(t *testing.T) {
		h := newRollbackHarness(t)
		h.engine.result = AttemptResult{AttemptID: "r1", Status: AttemptCanceled}

		err := h.service.Run(context.Background(), RollbackPlan{DeploymentID: idB})

		assert.ErrorIs(t, err, ErrAttemptCanceled)
	})
	t.Run("el motor cancela antes de abrir el intento", func(t *testing.T) {
		h := newRollbackHarness(t)
		h.engine.err = &EngineError{Kind: EngineCanceled}

		err := h.service.Run(context.Background(), RollbackPlan{DeploymentID: idB})

		assert.ErrorIs(t, err, ErrAttemptCanceled)
	})
	t.Run("Ctrl+C durante el clonado", func(t *testing.T) {
		h := newRollbackHarness(t)
		h.sources.err = errors.New("signal: killed")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := h.service.Run(ctx, RollbackPlan{DeploymentID: idB})

		assert.ErrorIs(t, err, ErrAttemptCanceled)
		assert.Zero(t, h.engine.attempts)
	})
}

func TestRollback_UnErrorDelMotorSePropaga(t *testing.T) {
	h := newRollbackHarness(t)
	h.engine.err = &EngineError{Kind: EngineNotFound, Message: "el despliegue x no existe"}

	err := h.service.Run(context.Background(), RollbackPlan{DeploymentID: idB})

	var engineErr *EngineError
	require.ErrorAs(t, err, &engineErr)
	assert.Equal(t, EngineNotFound, engineErr.Kind)
	assert.Empty(t, h.presenter.results, "sin resultado no hay resumen")
}

func TestRollback_SiFallaElClonadoNoSeLlamaAlMotor(t *testing.T) {
	h := newRollbackHarness(t)
	h.sources.err = errors.New("sin acceso")

	err := h.service.Run(context.Background(), RollbackPlan{DeploymentID: idB})

	assert.ErrorContains(t, err, "sin acceso")
	assert.Zero(t, h.engine.attempts)
}
