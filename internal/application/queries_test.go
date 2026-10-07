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

type fakeQueryEngine struct {
	attempts    []AttemptSummary
	detail      AttemptDetail
	outputs     []CommandOutput
	deployments []Deployment
	releases    []Release
	err         error

	specs      []ContainerSpec
	asked      []string // «método:argumento»
	logRequest LogsRequest
}

func (f *fakeQueryEngine) record(spec ContainerSpec, call string) {
	f.specs = append(f.specs, spec)
	f.asked = append(f.asked, call)
}

func (f *fakeQueryEngine) Attempts(_ context.Context, spec ContainerSpec, environment string) ([]AttemptSummary, error) {
	f.record(spec, "Attempts:"+environment)
	return f.attempts, f.err
}

func (f *fakeQueryEngine) AttemptDetail(_ context.Context, spec ContainerSpec, id string) (AttemptDetail, error) {
	f.record(spec, "AttemptDetail:"+id)
	return f.detail, f.err
}

func (f *fakeQueryEngine) Logs(_ context.Context, spec ContainerSpec, req LogsRequest) ([]CommandOutput, error) {
	f.record(spec, "Logs:"+req.AttemptID)
	f.logRequest = req
	return f.outputs, f.err
}

func (f *fakeQueryEngine) Deployments(_ context.Context, spec ContainerSpec, environment string) ([]Deployment, error) {
	f.record(spec, "Deployments:"+environment)
	return f.deployments, f.err
}

func (f *fakeQueryEngine) Releases(_ context.Context, spec ContainerSpec, environment string) ([]Release, error) {
	f.record(spec, "Releases:"+environment)
	return f.releases, f.err
}

type queryHarness struct {
	service *QueryService
	engine  *fakeQueryEngine
	sources *fakeSources
	recents *RecentsService
	store   *memoryStore
}

func newQueryHarness(t *testing.T) *queryHarness {
	t.Helper()
	base := t.TempDir()
	sources := &fakeSources{}
	workspace := NewWorkspace(
		&stubProjectRepo{loaded: newProject(t), existsBool: true}, sources, &fakeImages{}, &fakePresenter{},
		LocalExecutorConfig{
			WorkDir: "/work", StoreDir: filepath.Join(base, "store"), SpaceDir: filepath.Join(base, "space"),
			MaterialDir: filepath.Join(base, "material"), Requester: "jailux",
		})
	store := newMemoryStore()
	recents := NewRecentsService(store)
	engine := &fakeQueryEngine{}
	return &queryHarness{
		service: NewQueryService(workspace, engine, recents), engine: engine, sources: sources, recents: recents, store: store,
	}
}

const projectKey = "seed-id" // el id del proyecto de newProject

func attempt(id, environment string, status AttemptStatus) AttemptSummary {
	return AttemptSummary{ID: id, Environment: environment, UntilStep: "deploy", Status: status, StartedAt: time.Now()}
}

func TestQueries_NingunaConsultaClonaNada(t *testing.T) {
	h := newQueryHarness(t)
	h.engine.attempts = []AttemptSummary{attempt(idA, "sand", AttemptSucceeded)}

	_, err := h.service.ListAttempts(context.Background(), "sand", 10)

	require.NoError(t, err)
	assert.Empty(t, h.sources.calls, "las consultas al historial no necesitan el proyecto ni el pipeline")
	require.Len(t, h.engine.specs, 1)
	assert.Equal(t, "/vex/almacen", h.engine.specs[0].Mounts[0].Target)
	assert.Len(t, h.engine.specs[0].Mounts, 1, "solo el historial montado")
}

func TestQueries_ListAttempts_LosMasRecientesPrimeroYConLimite(t *testing.T) {
	h := newQueryHarness(t)
	// El motor los devuelve del más antiguo al más reciente.
	h.engine.attempts = []AttemptSummary{
		attempt(idA, "sand", AttemptSucceeded), attempt(idB, "sand", AttemptFailed), attempt(idC, "sand", AttemptSucceeded),
	}

	list, err := h.service.ListAttempts(context.Background(), "sand", 2)

	require.NoError(t, err)
	assert.Equal(t, 3, list.Total)
	require.Len(t, list.Attempts, 2)
	assert.Equal(t, []string{idC, idB}, []string{list.Attempts[0].ID, list.Attempts[1].ID})
	assert.Equal(t, "sand", list.Environment)
}

func TestQueries_ListAttempts_SinLimiteDevuelveTodos(t *testing.T) {
	h := newQueryHarness(t)
	h.engine.attempts = []AttemptSummary{attempt(idA, "sand", ""), attempt(idB, "sand", "")}

	list, err := h.service.ListAttempts(context.Background(), "sand", 0)

	require.NoError(t, err)
	assert.Len(t, list.Attempts, 2)
}

func TestQueries_ListAttempts_RecuerdaLosIntentosVistos(t *testing.T) {
	h := newQueryHarness(t)
	h.engine.attempts = []AttemptSummary{attempt(idA, "sand", AttemptFailed)}

	_, err := h.service.ListAttempts(context.Background(), "sand", 10)
	require.NoError(t, err)

	got, err := h.recents.ResolveAttempt(projectKey, "3d0dc694")
	require.NoError(t, err, "`vex why 3d0dc694` funciona sobre un intento que solo se vio en una lista")
	assert.Equal(t, idA, got)
}

func TestQueries_UnAmbienteQueNoExisteSeAvisaConSugerencia(t *testing.T) {
	h := newQueryHarness(t)
	require.NoError(t, h.recents.RememberCatalog(projectKey,
		[]Environment{{Value: "sand"}, {Value: "stag"}, {Value: "prod"}}, nil))

	_, err := h.service.ListAttempts(context.Background(), "sandd", 10)

	var unknown *UnknownEnvironmentError
	require.ErrorAs(t, err, &unknown)
	assert.Equal(t, "sandd", unknown.Name)
	assert.Equal(t, "sand", unknown.Suggestion)
	assert.Equal(t, []string{"sand", "stag", "prod"}, unknown.Known)
	assert.Empty(t, h.engine.asked, "ni siquiera se le pregunta al motor")
}

func TestQueries_SinCatalogoRecordadoNoSeOpinaDelAmbiente(t *testing.T) {
	h := newQueryHarness(t)

	list, err := h.service.ListAttempts(context.Background(), "lo-que-sea", 10)

	require.NoError(t, err)
	assert.Empty(t, list.Attempts)
}

func TestQueries_ShowAttempt_ResuelveElFinalDelId(t *testing.T) {
	h := newQueryHarness(t)
	require.NoError(t, h.recents.RememberAttempt(projectKey, RecentAttempt{ID: idB}))
	h.engine.detail = AttemptDetail{AttemptSummary: AttemptSummary{ID: idB, Environment: "sand", Status: AttemptFailed}}

	detail, err := h.service.ShowAttempt(context.Background(), "f974d0dee")

	require.NoError(t, err)
	assert.Equal(t, []string{"AttemptDetail:" + idB}, h.engine.asked, "al motor siempre le llega el id completo")
	assert.Equal(t, idB, detail.ID)
}

func TestQueries_ShowAttempt_SinIdUsaElUltimoRecordado(t *testing.T) {
	h := newQueryHarness(t)
	require.NoError(t, h.recents.RememberAttempt(projectKey, RecentAttempt{ID: idA}))
	require.NoError(t, h.recents.RememberAttempt(projectKey, RecentAttempt{ID: idC}))

	_, err := h.service.ShowAttempt(context.Background(), "")

	require.NoError(t, err)
	assert.Equal(t, []string{"AttemptDetail:" + idC}, h.engine.asked)
}

func TestQueries_ShowAttempt_SinNadaRecordado(t *testing.T) {
	h := newQueryHarness(t)

	_, err := h.service.ShowAttempt(context.Background(), "")

	assert.ErrorIs(t, err, ErrNoRecentAttempt)
	assert.Empty(t, h.engine.asked)
}

func TestQueries_ShowAttempt_ErroresDeId(t *testing.T) {
	h := newQueryHarness(t)
	require.NoError(t, h.recents.RememberAttempt(projectKey, RecentAttempt{ID: idA}))

	_, errShort := h.service.ShowAttempt(context.Background(), "abc")
	_, errUnknown := h.service.ShowAttempt(context.Background(), "zzzzzzz")

	assert.ErrorIs(t, errShort, ErrIDTooShort)
	assert.ErrorIs(t, errUnknown, ErrUnknownID)
	assert.Empty(t, h.engine.asked, "un id mal tecleado no llega al motor")
}

func TestQueries_Logs(t *testing.T) {
	t.Run("con el id y el filtro de fallidos", func(t *testing.T) {
		h := newQueryHarness(t)
		require.NoError(t, h.recents.RememberAttempt(projectKey, RecentAttempt{ID: idB}))
		h.engine.outputs = []CommandOutput{{Step: "test", Command: "mvn", Text: "boom"}}

		logs, err := h.service.Logs(context.Background(), "f974d0dee", true)

		require.NoError(t, err)
		assert.Equal(t, LogsRequest{AttemptID: idB, OnlyFailed: true}, h.engine.logRequest)
		assert.Equal(t, idB, logs.AttemptID)
		assert.Equal(t, h.engine.outputs, logs.Outputs)
	})

	t.Run("sin id usa el último recordado", func(t *testing.T) {
		h := newQueryHarness(t)
		require.NoError(t, h.recents.RememberAttempt(projectKey, RecentAttempt{ID: idC}))

		_, err := h.service.Logs(context.Background(), "", false)

		require.NoError(t, err)
		assert.Equal(t, idC, h.engine.logRequest.AttemptID)
	})

	t.Run("sin id y sin memoria deja que el motor elija el último", func(t *testing.T) {
		h := newQueryHarness(t)

		_, err := h.service.Logs(context.Background(), "", false)

		require.NoError(t, err)
		assert.Empty(t, h.engine.logRequest.AttemptID)
	})
}

func TestQueries_ListDeployments_MarcaElLanzadoYVaDelMasRecienteAlMasAntiguo(t *testing.T) {
	h := newQueryHarness(t)
	h.engine.deployments = []Deployment{
		{ID: idA, Environment: "prod"}, {ID: idB, Environment: "prod"}, {ID: idC, Environment: "prod"},
	}
	// B se lanzó, luego C, y luego se volvió a lanzar B (un rollback visible): el actual es B.
	h.engine.releases = []Release{
		{ID: "l1", DeploymentID: idB, Version: 1, Name: "v1"},
		{ID: "l2", DeploymentID: idC, Version: 2, Name: "v2"},
		{ID: "l3", DeploymentID: idB, Version: 1, Name: "v1-de-nuevo"},
	}

	list, err := h.service.ListDeployments(context.Background(), "prod", 10)

	require.NoError(t, err)
	require.Len(t, list.Items, 3)
	assert.Equal(t, []string{idC, idB, idA}, []string{list.Items[0].Deployment.ID, list.Items[1].Deployment.ID, list.Items[2].Deployment.ID})
	assert.False(t, list.Items[0].Current)
	assert.True(t, list.Items[1].Current, "el último lanzamiento del ambiente apunta a B")
	require.NotNil(t, list.Items[1].Release)
	assert.Equal(t, "v1-de-nuevo", list.Items[1].Release.Name, "se muestra el último lanzamiento de ese despliegue")
	assert.Nil(t, list.Items[2].Release, "A nunca se lanzó")
	assert.Equal(t, 3, list.Total)
}

func TestQueries_ListDeployments_RecuerdaLosIdsParaElRollback(t *testing.T) {
	h := newQueryHarness(t)
	h.engine.deployments = []Deployment{{ID: idB, Environment: "prod"}}

	_, err := h.service.ListDeployments(context.Background(), "prod", 10)
	require.NoError(t, err)

	got, err := h.recents.ResolveDeployment(projectKey, "f974d0dee")
	require.NoError(t, err)
	assert.Equal(t, idB, got)
}

func TestQueries_ListDeployments_SinLanzamientosNingunoEsElActual(t *testing.T) {
	h := newQueryHarness(t)
	h.engine.deployments = []Deployment{{ID: idA, Environment: "sand"}}

	list, err := h.service.ListDeployments(context.Background(), "sand", 10)

	require.NoError(t, err)
	assert.False(t, list.Items[0].Current)
	assert.Nil(t, list.Items[0].Release)
}

func TestQueries_ListReleases_DelMasRecienteAlMasAntiguo(t *testing.T) {
	h := newQueryHarness(t)
	h.engine.releases = []Release{{ID: "l1", Name: "v1"}, {ID: "l2", Name: "v2"}, {ID: "l3", Name: "v3"}}

	list, err := h.service.ListReleases(context.Background(), "prod", 2)

	require.NoError(t, err)
	assert.Equal(t, 3, list.Total)
	assert.Equal(t, []string{"l3", "l2"}, []string{list.Releases[0].ID, list.Releases[1].ID})
}

func TestQueries_UnErrorDelMotorSePropaga(t *testing.T) {
	h := newQueryHarness(t)
	h.engine.err = &EngineError{Kind: EngineNotFound}

	_, err := h.service.ShowAttempt(context.Background(), idA)

	var engineErr *EngineError
	require.True(t, errors.As(err, &engineErr))
	assert.Equal(t, EngineNotFound, engineErr.Kind)
}

func TestQueries_ProyectoNoInicializado(t *testing.T) {
	h := newQueryHarness(t)
	h.service.workspace.projects = &stubProjectRepo{existsBool: false}

	_, err := h.service.ListAttempts(context.Background(), "sand", 10)

	assert.ErrorContains(t, err, MessageProjectNotInitialized)
}

func TestQueries_ListAttempts_MarcaComoAbandonadosLosQueEsteCLIAbandono(t *testing.T) {
	h := newQueryHarness(t)
	require.NoError(t, h.recents.RememberAttempt(projectKey, RecentAttempt{ID: idB, Abandoned: true}))
	h.engine.attempts = []AttemptSummary{attempt(idA, "sand", ""), attempt(idB, "sand", "")}

	list, err := h.service.ListAttempts(context.Background(), "sand", 10)

	require.NoError(t, err)
	byID := map[string]AttemptSummary{}
	for _, a := range list.Attempts {
		byID[a.ID] = a
	}
	assert.True(t, byID[idA].InProgress(), "el que no se abandonó sigue en curso")
	assert.True(t, byID[idB].Abandoned)
	assert.False(t, byID[idB].InProgress(), "uno abandonado ya no ocupa nada: no está «en curso»")
}
