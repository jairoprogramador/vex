package engine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jairoprogramador/vex/internal/application"
)

// sentRequest es la línea JSON-RPC que el "docker" falso recibió por stdin.
type sentRequest struct {
	Method  string
	Params  map[string]any
	Entorno map[string]string
}

func (f fakeDocker) sent(t *testing.T) sentRequest {
	t.Helper()
	var req sentRequest
	require.NoError(t, json.Unmarshal([]byte(f.readFile(t, "request.json")), &req))
	return req
}

func result(body string) string {
	return `{"jsonrpc":"2.0","id":"1","result":` + body + `}`
}

func TestOperations_EnvianElMetodoYLosParametrosDelMotor(t *testing.T) {
	ctx := context.Background()
	spec := application.ContainerSpec{Image: "img"}

	t.Run("intentos", func(t *testing.T) {
		fake := newFakeDockerReplying(t, result(`[{"Id":"i1","Ambiente":"sand","Solicitante":"ana","HastaPaso":"test","Instante":"2026-10-06T12:00:00Z","Estado":"exitoso","Causa":""}]`))

		got, err := fake.engine.Attempts(ctx, spec, "sand")

		require.NoError(t, err)
		assert.Equal(t, sentRequest{Method: "intentos", Params: map[string]any{"Version": "1", "Ambiente": "sand"}}, fake.sent(t))
		require.Len(t, got, 1)
		assert.Equal(t, application.AttemptSucceeded, got[0].Status)
		assert.Equal(t, "ana", got[0].Requester)
	})

	t.Run("intento", func(t *testing.T) {
		fake := newFakeDockerReplying(t, result(`{"Id":"i1","Apertura":{"Ambiente":"sand","Pasos":[{"Nombre":"test"}],"HastaPaso":"test","Contenido":{}},"Instante":"2026-10-06T12:00:00Z","Registros":[],"Estado":"","Causa":"","Destino":"","Abandonado":false}`))

		got, err := fake.engine.AttemptDetail(ctx, spec, "i1")

		require.NoError(t, err)
		assert.Equal(t, sentRequest{Method: "intento", Params: map[string]any{"Version": "1", "Intento": "i1"}}, fake.sent(t))
		assert.True(t, got.InProgress())
		assert.Equal(t, []application.StepOutcome{{Name: "test", Status: application.StepPending}}, got.Steps)
	})

	t.Run("despliegues y lanzamientos", func(t *testing.T) {
		fake := newFakeDockerReplying(t, result(`[{"Id":"d1","Ambiente":"prod","Intento":"i1","Padre":"d0","Instante":"2026-10-06T12:00:00Z"}]`))
		deployments, err := fake.engine.Deployments(ctx, spec, "prod")
		require.NoError(t, err)
		assert.Equal(t, "despliegues", fake.sent(t).Method)
		assert.Equal(t, []application.Deployment{{ID: "d1", Environment: "prod", AttemptID: "i1", ParentID: "d0", At: at0}}, deployments)

		fake = newFakeDockerReplying(t, result(`[{"Id":"l1","Ambiente":"prod","Despliegue":"d1","Version":3,"Nombre":"v3","Instante":"2026-10-06T12:00:00Z"}]`))
		releases, err := fake.engine.Releases(ctx, spec, "prod")
		require.NoError(t, err)
		assert.Equal(t, "lanzamientos", fake.sent(t).Method)
		assert.Equal(t, []application.Release{{ID: "l1", Environment: "prod", DeploymentID: "d1", Version: 3, Name: "v3", At: at0}}, releases)
	})

	t.Run("ambientes y pasos llevan el pipeline", func(t *testing.T) {
		pipeline := application.PipelineRef{Source: "/pipeline", Commit: "abc"}

		fake := newFakeDockerReplying(t, result(`[{"Nombre":"production","Descripcion":"d","Valor":"prod","Reservado":true}]`))
		environments, err := fake.engine.Environments(ctx, spec, pipeline)
		require.NoError(t, err)
		assert.Equal(t, sentRequest{Method: "ambientes", Params: map[string]any{
			"Version": "1", "FuenteDelPipeline": "/pipeline", "Commit": "abc"}}, fake.sent(t))
		assert.Equal(t, []application.Environment{{Name: "production", Description: "d", Value: "prod", Protected: true}}, environments)

		fake = newFakeDockerReplying(t, result(`[{"Nombre":"test","Orden":1,"Compartido":true}]`))
		steps, err := fake.engine.Steps(ctx, spec, pipeline)
		require.NoError(t, err)
		assert.Equal(t, "pasos", fake.sent(t).Method)
		assert.Equal(t, []application.PipelineStep{{Name: "test", Order: 1, Shared: true}}, steps)
	})

	t.Run("simular", func(t *testing.T) {
		fake := newFakeDockerReplying(t, result(`{"Ambiente":"sand","Solicitante":"ana","HastaPaso":"deploy","Estado":"exitoso"}`))

		got, err := fake.engine.Check(ctx, spec, application.CheckRequest{
			Environment: "sand", Requester: "ana", UntilStep: "deploy",
			Pipeline: application.PipelineRef{Source: "/pipeline", Commit: "abc"},
			Project:  application.ProjectMetadata{ID: "p1", Name: "demo"},
		})

		require.NoError(t, err)
		sent := fake.sent(t)
		assert.Equal(t, "simular", sent.Method)
		assert.Equal(t, "/pipeline", sent.Params["Fuente"])
		assert.Equal(t, "abc", sent.Params["Commit"])
		assert.Equal(t, "deploy", sent.Params["HastaPaso"])
		assert.Equal(t, map[string]any{"ProjectId": "p1", "ProjectName": "demo", "ProjectOrganization": "", "ProjectTeam": ""}, sent.Params["Metadatos"])
		assert.True(t, got.Valid)
	})

	t.Run("lanzar, reservar y liberar", func(t *testing.T) {
		fake := newFakeDockerReplying(t, result(`{"Id":"l9","Ambiente":"prod","Despliegue":"d1","Version":4,"Nombre":"v4","Instante":"2026-10-06T12:00:00Z"}`))
		release, err := fake.engine.Release(ctx, spec, application.ReleaseRequest{Environment: "prod", DeploymentID: "d1", Name: "v4"})
		require.NoError(t, err)
		assert.Equal(t, sentRequest{Method: "lanzar", Params: map[string]any{
			"Version": "1", "Ambiente": "prod", "Despliegue": "d1", "Nombre": "v4"}}, fake.sent(t))
		assert.Equal(t, "l9", release.ID)

		fake = newFakeDockerReplying(t, result(`{}`))
		require.NoError(t, fake.engine.Protect(ctx, spec, "prod"))
		assert.Equal(t, sentRequest{Method: "reservar", Params: map[string]any{"Version": "1", "Ambiente": "prod"}}, fake.sent(t))

		fake = newFakeDockerReplying(t, result(`{}`))
		require.NoError(t, fake.engine.Unprotect(ctx, spec, "prod"))
		assert.Equal(t, "liberar", fake.sent(t).Method)
	})

	t.Run("diagnosticar y abandonar", func(t *testing.T) {
		fake := newFakeDockerReplying(t, result(`{"SinDiagnostico":"no_se_atribuye"}`))
		diagnosis, err := fake.engine.Diagnose(ctx, spec, application.DiagnoseRequest{AttemptID: "i1", ReferenceDeploymentID: "d0"})
		require.NoError(t, err)
		assert.Equal(t, sentRequest{Method: "diagnosticar", Params: map[string]any{
			"Version": "1", "Intento": "i1", "Lanzamiento": "", "Ambiente": "", "Referencia": "d0"}}, fake.sent(t))
		assert.Equal(t, application.DiagnosisNotAttributable, diagnosis.Kind)

		fake = newFakeDockerReplying(t, result(`{}`))
		require.NoError(t, fake.engine.Abandon(ctx, spec, "i1"))
		assert.Equal(t, sentRequest{Method: "abandonar", Params: map[string]any{"Version": "1", "Intento": "i1"}}, fake.sent(t))
	})

	t.Run("rollback emite progreso como un intento", func(t *testing.T) {
		fake := newFakeDocker(t, "success")
		var events []application.EngineEvent

		got, err := fake.engine.Rollback(ctx, spec, application.RollbackRequest{
			DeploymentID: "d1", Requester: "ana", Project: application.ProjectMetadata{ID: "p1"},
		}, func(e application.EngineEvent) { events = append(events, e) })

		require.NoError(t, err)
		sent := fake.sent(t)
		assert.Equal(t, "rollback", sent.Method)
		assert.Equal(t, "d1", sent.Params["Despliegue"])
		assert.Equal(t, "ana", sent.Params["Solicitante"])
		assert.Equal(t, application.AttemptSucceeded, got.Status)
		assert.NotEmpty(t, events)
	})
}

func TestOperations_UnErrorDelMotorLlegaTraducido(t *testing.T) {
	fake := newFakeDockerReplying(t,
		`{"jsonrpc":"2.0","id":"1","error":{"code":-32602,"message":"x","data":{"tipo":"parametros_invalidos","campo":"Ambiente","valor":"nope"}}}`)

	_, err := fake.engine.Attempts(context.Background(), application.ContainerSpec{Image: "img"}, "nope")

	var engineErr *application.EngineError
	require.ErrorAs(t, err, &engineErr)
	assert.Equal(t, application.EngineInvalidParams, engineErr.Kind)
	assert.Equal(t, "Ambiente", engineErr.Field)
	assert.Equal(t, "nope", engineErr.Value)
}
