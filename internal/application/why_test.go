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

type fakeDiagnosisEngine struct {
	detail    AttemptDetail
	detailErr error
	diagnosis Diagnosis
	diagErr   error

	calls   []string
	request DiagnoseRequest
}

func (f *fakeDiagnosisEngine) AttemptDetail(_ context.Context, _ ContainerSpec, id string) (AttemptDetail, error) {
	f.calls = append(f.calls, "AttemptDetail:"+id)
	return f.detail, f.detailErr
}

func (f *fakeDiagnosisEngine) Diagnose(_ context.Context, _ ContainerSpec, req DiagnoseRequest) (Diagnosis, error) {
	f.calls = append(f.calls, "Diagnose:"+req.AttemptID)
	f.request = req
	return f.diagnosis, f.diagErr
}

type whyHarness struct {
	service *DiagnosisService
	engine  *fakeDiagnosisEngine
	recents *RecentsService
}

func newWhyHarness(t *testing.T) *whyHarness {
	t.Helper()
	base := t.TempDir()
	workspace := NewWorkspace(
		&stubProjectRepo{loaded: newProject(t), existsBool: true}, &fakeSources{}, &fakeImages{}, &fakePresenter{},
		LocalExecutorConfig{
			WorkDir: "/work", StoreDir: filepath.Join(base, "store"), SpaceDir: filepath.Join(base, "space"),
			MaterialDir: filepath.Join(base, "material"),
		})
	engine := &fakeDiagnosisEngine{}
	recents := NewRecentsService(newMemoryStore())
	return &whyHarness{service: NewDiagnosisService(workspace, engine, recents), engine: engine, recents: recents}
}

func (h *whyHarness) remember(t *testing.T, a RecentAttempt) {
	t.Helper()
	require.NoError(t, h.recents.RememberAttempt(projectKey, a))
}

func found() Diagnosis {
	return Diagnosis{
		Kind: DiagnosisFound, Environment: "sand", AttemptsSince: 2,
		Reference: DiagnosedAttempt{ID: idA, At: time.Now()}, Failed: DiagnosedAttempt{ID: idB, At: time.Now()},
		Changes: DiagnosisChanges{Instructions: []string{"deploy"}},
	}
}

func TestWhy_UnFallidoConocidoSeDiagnosticaSinPreguntarPorElDetalle(t *testing.T) {
	h := newWhyHarness(t)
	h.remember(t, RecentAttempt{ID: idB, Environment: "sand", Status: AttemptFailed})
	h.engine.diagnosis = found()

	report, err := h.service.Why(context.Background(), "f974d0dee", "")

	require.NoError(t, err)
	assert.Equal(t, []string{"Diagnose:" + idB}, h.engine.calls, "sabe que falló: va directo al diagnóstico")
	assert.Equal(t, WhyDiagnosed, report.Outcome)
	assert.Equal(t, "sand", report.Environment)
	assert.Equal(t, []string{"deploy"}, report.Diagnosis.Changes.Instructions)
}

func TestWhy_SinIdUsaElUltimoFallidoNoElUltimoIntento(t *testing.T) {
	h := newWhyHarness(t)
	h.remember(t, RecentAttempt{ID: idA, Environment: "sand", Status: AttemptFailed})
	h.remember(t, RecentAttempt{ID: idC, Environment: "sand", Status: AttemptSucceeded})
	h.engine.diagnosis = found()

	report, err := h.service.Why(context.Background(), "", "")

	require.NoError(t, err)
	assert.Equal(t, idA, report.AttemptID, "el exitoso más reciente no es lo que se quiere explicar")
}

func TestWhy_SinNingunFallidoRecordado(t *testing.T) {
	h := newWhyHarness(t)
	h.remember(t, RecentAttempt{ID: idA, Status: AttemptSucceeded})

	_, err := h.service.Why(context.Background(), "", "")

	assert.ErrorIs(t, err, ErrNoFailedAttempt)
	assert.Empty(t, h.engine.calls)
}

func TestWhy_CompararConUnDespliegueElegidoAMano(t *testing.T) {
	h := newWhyHarness(t)
	h.remember(t, RecentAttempt{ID: idB, Status: AttemptFailed})
	require.NoError(t, h.recents.RememberDeployments(projectKey, []Deployment{{ID: idA, Environment: "sand"}}))
	h.engine.diagnosis = found()

	_, err := h.service.Why(context.Background(), "f974d0dee", "3d0dc694")

	require.NoError(t, err)
	assert.Equal(t, DiagnoseRequest{AttemptID: idB, ReferenceDeploymentID: idA}, h.engine.request,
		"el sufijo del despliegue se resuelve al id completo")
}

func TestWhy_UnDespliegueDeReferenciaDesconocido(t *testing.T) {
	h := newWhyHarness(t)
	h.remember(t, RecentAttempt{ID: idB, Status: AttemptFailed})

	_, err := h.service.Why(context.Background(), "f974d0dee", "zzzzzzz")

	assert.ErrorIs(t, err, ErrUnknownID)
	assert.Empty(t, h.engine.calls)
}

func TestWhy_LoQueNoEsUnFalloNoSeDiagnostica(t *testing.T) {
	tests := []struct {
		name  string
		known RecentAttempt
		want  WhyOutcome
	}{
		{"salió bien", RecentAttempt{ID: idB, Status: AttemptSucceeded}, WhyNotAFailure},
		{"se canceló", RecentAttempt{ID: idB, Status: AttemptCanceled}, WhyCanceled},
		{"se interrumpió", RecentAttempt{ID: idB, Status: AttemptFailed, Cause: CauseInterrupted}, WhyInterrupted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newWhyHarness(t)
			h.remember(t, tt.known)

			report, err := h.service.Why(context.Background(), "f974d0dee", "")

			require.NoError(t, err)
			assert.Equal(t, tt.want, report.Outcome)
			assert.Empty(t, h.engine.calls, "no se le pide nada al motor: ya se sabe")
		})
	}
}

func TestWhy_SiNoSabeComoTerminoPreguntaPorElDetalleYDecide(t *testing.T) {
	tests := []struct {
		name      string
		detail    AttemptDetail
		want      WhyOutcome
		diagnoses bool
	}{
		{"en realidad falló: se diagnostica", AttemptDetail{AttemptSummary: AttemptSummary{ID: idB, Environment: "sand", Status: AttemptFailed}}, WhyDiagnosed, true},
		{"en curso", AttemptDetail{AttemptSummary: AttemptSummary{ID: idB, Environment: "sand"}}, WhyInProgress, false},
		{"abandonado", AttemptDetail{AttemptSummary: AttemptSummary{ID: idB, Abandoned: true}}, WhyNotAttributable, false},
		{"salió bien", AttemptDetail{AttemptSummary: AttemptSummary{ID: idB, Status: AttemptSucceeded}}, WhyNotAFailure, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newWhyHarness(t)
			h.engine.detail = tt.detail
			h.engine.diagnosis = found()

			report, err := h.service.Why(context.Background(), idB, "") // id completo: no está en la memoria

			require.NoError(t, err)
			assert.Equal(t, tt.want, report.Outcome)
			assert.Equal(t, tt.diagnoses, len(h.engine.calls) == 2, "calls: %v", h.engine.calls)
			known, ok, _ := h.recents.FindAttempt(projectKey, idB)
			require.True(t, ok, "lo averiguado se recuerda")
			assert.Equal(t, tt.detail.Status, known.Status)
		})
	}
}

func TestWhy_LasDosFormasDeNoHayDiagnosticoDelMotor(t *testing.T) {
	h := newWhyHarness(t)
	h.remember(t, RecentAttempt{ID: idB, Environment: "sand", Status: AttemptFailed})

	h.engine.diagnosis = Diagnosis{Kind: DiagnosisNoReference}
	report, err := h.service.Why(context.Background(), "f974d0dee", "")
	require.NoError(t, err)
	assert.Equal(t, WhyNoReference, report.Outcome)
	assert.Equal(t, "sand", report.Environment, "el ambiente sale de lo recordado")

	h.engine.diagnosis = Diagnosis{Kind: DiagnosisNotAttributable}
	report, err = h.service.Why(context.Background(), "f974d0dee", "")
	require.NoError(t, err)
	assert.Equal(t, WhyNotAttributable, report.Outcome)
}

func TestWhy_UnErrorDelMotorSePropaga(t *testing.T) {
	h := newWhyHarness(t)
	h.remember(t, RecentAttempt{ID: idB, Status: AttemptFailed})
	h.engine.diagErr = &EngineError{Kind: EngineNotFound}

	_, err := h.service.Why(context.Background(), "f974d0dee", "")

	var engineErr *EngineError
	require.True(t, errors.As(err, &engineErr))
	assert.Equal(t, EngineNotFound, engineErr.Kind)
}
