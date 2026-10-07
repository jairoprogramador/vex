package application

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAbandonEngine struct {
	detail    AttemptDetail
	detailErr error
	abandonEr error

	calls []string
}

func (f *fakeAbandonEngine) AttemptDetail(_ context.Context, _ ContainerSpec, id string) (AttemptDetail, error) {
	f.calls = append(f.calls, "AttemptDetail:"+id)
	return f.detail, f.detailErr
}

func (f *fakeAbandonEngine) Abandon(_ context.Context, _ ContainerSpec, id string) error {
	f.calls = append(f.calls, "Abandon:"+id)
	return f.abandonEr
}

type abandonHarness struct {
	service *AbandonService
	engine  *fakeAbandonEngine
	recents *RecentsService
}

func newAbandonHarness(t *testing.T) *abandonHarness {
	t.Helper()
	base := t.TempDir()
	workspace := NewWorkspace(
		&stubProjectRepo{loaded: newProject(t), existsBool: true}, &fakeSources{}, &fakeImages{}, &fakePresenter{},
		LocalExecutorConfig{
			WorkDir: "/work", StoreDir: filepath.Join(base, "store"), SpaceDir: filepath.Join(base, "space"),
			MaterialDir: filepath.Join(base, "material"),
		})
	engine := &fakeAbandonEngine{}
	recents := NewRecentsService(newMemoryStore())
	return &abandonHarness{service: NewAbandonService(workspace, engine, recents), engine: engine, recents: recents}
}

func openDetail(id string) AttemptDetail {
	return AttemptDetail{AttemptSummary: AttemptSummary{ID: id, Environment: "sand", UntilStep: "deploy"}}
}

func TestAbandon_ElPlanVerificaConElMotorQueSigueSinTerminar(t *testing.T) {
	h := newAbandonHarness(t)
	require.NoError(t, h.recents.RememberAttempt(projectKey, RecentAttempt{ID: idB}))
	h.engine.detail = openDetail(idB)

	plan, err := h.service.Plan(context.Background(), "f974d0dee")

	require.NoError(t, err)
	assert.Equal(t, idB, plan.Attempt.ID)
	assert.Equal(t, "sand", plan.Attempt.Environment)
	assert.Equal(t, []string{"AttemptDetail:" + idB}, h.engine.calls, "planificar no abandona nada")
}

func TestAbandon_SinIdUsaElUnicoIntentoSinTerminar(t *testing.T) {
	h := newAbandonHarness(t)
	require.NoError(t, h.recents.RememberAttempt(projectKey, RecentAttempt{ID: idA, Status: AttemptSucceeded}))
	require.NoError(t, h.recents.RememberAttempt(projectKey, RecentAttempt{ID: idB}))
	h.engine.detail = openDetail(idB)

	plan, err := h.service.Plan(context.Background(), "")

	require.NoError(t, err)
	assert.Equal(t, idB, plan.Attempt.ID)
}

func TestAbandon_SinIdYSinNingunoSinTerminar(t *testing.T) {
	h := newAbandonHarness(t)
	require.NoError(t, h.recents.RememberAttempt(projectKey, RecentAttempt{ID: idA, Status: AttemptSucceeded}))

	_, err := h.service.Plan(context.Background(), "")

	assert.ErrorIs(t, err, ErrNoOpenAttempt)
	assert.Empty(t, h.engine.calls)
}

func TestAbandon_ConVariosSinTerminarNoAdivina(t *testing.T) {
	h := newAbandonHarness(t)
	require.NoError(t, h.recents.RememberAttempt(projectKey, RecentAttempt{ID: idA}))
	require.NoError(t, h.recents.RememberAttempt(projectKey, RecentAttempt{ID: idB}))

	_, err := h.service.Plan(context.Background(), "")

	var several *OpenAttemptsError
	require.ErrorAs(t, err, &several)
	assert.Len(t, several.Candidates, 2)
	assert.Empty(t, h.engine.calls, "abandonar uno equivocado liberaría un ambiente que alguien usa")
}

func TestAbandon_UnIntentoQueYaTerminoNoSeAbandona(t *testing.T) {
	tests := []struct {
		name   string
		detail AttemptDetail
	}{
		{"terminó bien", AttemptDetail{AttemptSummary: AttemptSummary{ID: idB, Environment: "sand", Status: AttemptSucceeded}}},
		{"terminó fallido", AttemptDetail{AttemptSummary: AttemptSummary{ID: idB, Environment: "sand", Status: AttemptFailed}}},
		{"ya estaba abandonado", AttemptDetail{AttemptSummary: AttemptSummary{ID: idB, Environment: "sand", Abandoned: true}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newAbandonHarness(t)
			h.engine.detail = tt.detail

			_, err := h.service.Plan(context.Background(), idB)

			var finished *AttemptFinishedError
			require.ErrorAs(t, err, &finished)
			assert.Equal(t, tt.detail.Abandoned, finished.Abandoned)
			assert.Equal(t, tt.detail.Status, finished.Status)
		})
	}
}

func TestAbandon_ConfirmadoAbandonaYLoRecuerda(t *testing.T) {
	h := newAbandonHarness(t)
	require.NoError(t, h.recents.RememberAttempt(projectKey, RecentAttempt{ID: idB}))
	h.engine.detail = openDetail(idB)
	plan, err := h.service.Plan(context.Background(), "f974d0dee")
	require.NoError(t, err)

	err = h.service.Abandon(context.Background(), plan)

	require.NoError(t, err)
	assert.Equal(t, []string{"AttemptDetail:" + idB, "Abandon:" + idB}, h.engine.calls)
	open, _ := h.recents.OpenAttempts(projectKey)
	assert.Empty(t, open, "un intento abandonado deja de ser candidato a abandonarse")
}

func TestAbandon_UnErrorDelMotorAlAbandonarSePropagaYNoSeRecuerda(t *testing.T) {
	h := newAbandonHarness(t)
	require.NoError(t, h.recents.RememberAttempt(projectKey, RecentAttempt{ID: idB}))
	h.engine.detail = openDetail(idB)
	plan, err := h.service.Plan(context.Background(), "f974d0dee")
	require.NoError(t, err)
	h.engine.abandonEr = &EngineError{Kind: EnginePipelineRejected} // se cerró mientras tanto

	err = h.service.Abandon(context.Background(), plan)

	var engineErr *EngineError
	require.True(t, errors.As(err, &engineErr))
	open, _ := h.recents.OpenAttempts(projectKey)
	assert.Len(t, open, 1, "no se marca como abandonado algo que el motor no abandonó")
}
