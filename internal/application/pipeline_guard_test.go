package application

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakePipelineEngine struct {
	result       CheckResult
	checkErr     error
	environments []Environment
	steps        []PipelineStep
	catalogErr   error

	calls []string
}

func (f *fakePipelineEngine) Check(_ context.Context, _ ContainerSpec, req CheckRequest) (CheckResult, error) {
	f.calls = append(f.calls, "Check:"+req.Environment+":"+req.UntilStep)
	return f.result, f.checkErr
}

func (f *fakePipelineEngine) Environments(context.Context, ContainerSpec, PipelineRef) ([]Environment, error) {
	f.calls = append(f.calls, "Environments")
	return f.environments, f.catalogErr
}

func (f *fakePipelineEngine) Steps(context.Context, ContainerSpec, PipelineRef) ([]PipelineStep, error) {
	f.calls = append(f.calls, "Steps")
	return f.steps, f.catalogErr
}

func newGuard(t *testing.T) (*PipelineGuard, *fakePipelineEngine, *RecentsService) {
	t.Helper()
	engine := &fakePipelineEngine{
		environments: []Environment{{Value: "sand"}, {Value: "stag"}, {Value: "prod"}},
		steps:        []PipelineStep{{Name: "test", Order: 1}, {Name: "package", Order: 2}, {Name: "deploy", Order: 3}},
	}
	recents := NewRecentsService(newMemoryStore())
	return NewPipelineGuard(engine, recents), engine, recents
}

func checkRequest(environment, step string) CheckRequest {
	return CheckRequest{Environment: environment, UntilStep: step, Pipeline: PipelineRef{Source: "/pipeline", Commit: "abc"}}
}

func invalidParams(field, value string) error {
	return &EngineError{Kind: EngineInvalidParams, Field: field, Value: value}
}

func TestGuard_TodoEnOrdenNoDevuelveNada(t *testing.T) {
	guard, engine, _ := newGuard(t)
	engine.result = CheckResult{Valid: true}

	err := guard.Check(context.Background(), ContainerSpec{}, "p", checkRequest("sand", "deploy"))

	assert.NoError(t, err)
	assert.Equal(t, []string{"Check:sand:deploy"}, engine.calls, "en el camino feliz solo se llama a check")
}

func TestGuard_UnPipelineConFallosDiceCuales(t *testing.T) {
	guard, engine, _ := newGuard(t)
	engine.result = CheckResult{Failures: []PipelineFailure{{Invariant: "formato", File: "config.yaml", Detail: "no se lee"}}}

	err := guard.Check(context.Background(), ContainerSpec{}, "p", checkRequest("sand", "deploy"))

	var failed *CheckFailedError
	require.ErrorAs(t, err, &failed)
	assert.Equal(t, "sand", failed.Environment)
	assert.Equal(t, "deploy", failed.UntilStep)
	assert.Equal(t, []PipelineFailure{{Invariant: "formato", File: "config.yaml", Detail: "no se lee"}}, failed.Failures)
	assert.Empty(t, failed.Missing)
}

func TestGuard_LasVariablesQueFaltan(t *testing.T) {
	guard, engine, _ := newGuard(t)
	engine.result = CheckResult{Missing: []MissingVariables{{Step: "deploy", Variables: []string{"db_url", "api_key"}}}}

	err := guard.Check(context.Background(), ContainerSpec{}, "p", checkRequest("sand", "deploy"))

	var failed *CheckFailedError
	require.ErrorAs(t, err, &failed)
	assert.Equal(t, []MissingVariables{{Step: "deploy", Variables: []string{"db_url", "api_key"}}}, failed.Missing)
}

func TestGuard_UnAmbienteQueNoExisteSeExplicaConLosQueSi(t *testing.T) {
	guard, engine, recents := newGuard(t)
	engine.checkErr = invalidParams("Ambiente", "sandd")
	require.NoError(t, recents.RememberSteps("p", []PipelineStep{{Name: "test"}}))

	err := guard.Check(context.Background(), ContainerSpec{}, "p", checkRequest("sandd", "deploy"))

	var unknown *UnknownEnvironmentError
	require.ErrorAs(t, err, &unknown)
	assert.Equal(t, "sandd", unknown.Name)
	assert.Equal(t, []string{"sand", "stag", "prod"}, unknown.Known)
	assert.Equal(t, "sand", unknown.Suggestion)
	assert.Equal(t, []string{"Check:sandd:deploy", "Environments"}, engine.calls,
		"solo se pregunta por los ambientes: el paso no hace falta para explicar esto")

	catalog, ok, _ := recents.Catalog("p")
	require.True(t, ok)
	assert.Len(t, catalog.Environments, 3, "lo aprendido se recuerda para los comandos que no preguntan al motor")
	assert.Len(t, catalog.Steps, 1, "y no se pisa lo que ya se sabía de los pasos")
}

func TestGuard_UnPasoQueNoExisteSeExplicaConLosQueSi(t *testing.T) {
	guard, engine, recents := newGuard(t)
	engine.checkErr = invalidParams("HastaPaso", "pakage")

	err := guard.Check(context.Background(), ContainerSpec{}, "p", checkRequest("sand", "pakage"))

	var unknown *UnknownStepError
	require.ErrorAs(t, err, &unknown)
	assert.Equal(t, "pakage", unknown.Name)
	assert.Equal(t, []string{"test", "package", "deploy"}, unknown.Known)
	assert.Equal(t, "package", unknown.Suggestion)
	catalog, _, _ := recents.Catalog("p")
	assert.Len(t, catalog.Steps, 3)
}

func TestGuard_SinPoderConsultarElCatalogoSeConservaElErrorDelMotor(t *testing.T) {
	guard, engine, _ := newGuard(t)
	engine.checkErr = invalidParams("Ambiente", "sandd")
	engine.catalogErr = errors.New("docker murió")

	err := guard.Check(context.Background(), ContainerSpec{}, "p", checkRequest("sandd", "deploy"))

	var engineErr *EngineError
	require.ErrorAs(t, err, &engineErr)
	assert.Equal(t, "sandd", engineErr.Value, "no se pierde lo que dijo el motor")
}

func TestGuard_OtrosErroresPasanTalCual(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"un parámetro inválido de otro campo", invalidParams("Solicitante", "")},
		{"un error de otro tipo", &EngineError{Kind: EngineNotFound}},
		{"un error que no es del motor", errors.New("boom")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			guard, engine, _ := newGuard(t)
			engine.checkErr = tt.err

			err := guard.Check(context.Background(), ContainerSpec{}, "p", checkRequest("sand", "deploy"))

			assert.Same(t, tt.err, err)
			assert.Equal(t, []string{"Check:sand:deploy"}, engine.calls, "no se pregunta por el catálogo si no hace falta")
		})
	}
}

func TestRecents_RecordarSoloLosPasosOSoloLosAmbientes(t *testing.T) {
	recents, _ := newRecents(t)

	require.NoError(t, recents.RememberEnvironments("p", []Environment{{Value: "sand"}}))
	require.NoError(t, recents.RememberSteps("p", []PipelineStep{{Name: "test"}}))
	require.NoError(t, recents.RememberEnvironments("p", []Environment{{Value: "sand"}, {Value: "prod"}}))

	catalog, ok, err := recents.Catalog("p")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Len(t, catalog.Environments, 2)
	assert.Len(t, catalog.Steps, 1)
	assert.False(t, catalog.UpdatedAt.IsZero())
}
