package engine

import (
	"context"

	"github.com/jairoprogramador/vex/internal/application"
	"github.com/jairoprogramador/vex/internal/infrastructure/engine/protocol"
)

// Las operaciones del motor, una por método. Todas siguen el mismo camino: una petición, un contenedor, un
// resultado que se decodifica y se traduce al lenguaje de la aplicación (invoke); lo único que cambia es el
// método JSON-RPC, sus parámetros y la traducción.

// invoke ejecuta una operación y decodifica su resultado. Si el motor responde con un error, lo traduce a
// *application.EngineError. onNotification recibe el progreso de las operaciones que lo emiten.
func invoke[T any](
	ctx context.Context, d *DockerEngine, spec application.ContainerSpec, method string, params any,
	secrets map[string]string, onNotification func(*protocol.Message),
) (T, error) {
	var out T
	reply, err := d.call(ctx, spec, protocol.NewRequest("1", method, params, secrets), onNotification)
	if err != nil {
		return out, err
	}
	if err := protocol.DecodeResult(reply.message, &out); err != nil {
		return out, translateError(err, reply.stderr)
	}
	return out, nil
}

// progress traduce cada notificación "progreso" a un evento de la aplicación.
func progress(onEvent func(application.EngineEvent)) func(*protocol.Message) {
	return func(msg *protocol.Message) {
		if event, ok := toEngineEvent(msg); ok && onEvent != nil {
			onEvent(event)
		}
	}
}

// --- AttemptRunner ---

func (d *DockerEngine) Attempt(
	ctx context.Context, spec application.ContainerSpec, req application.AttemptRequest, onEvent func(application.EngineEvent),
) (application.AttemptResult, error) {
	result, err := invoke[protocol.Resultado](
		ctx, d, spec, protocol.MethodIntentar, toAttemptParams(req), req.Secrets, progress(onEvent))
	if err != nil {
		return application.AttemptResult{}, err
	}
	return toAttemptResult(result), nil
}

func (d *DockerEngine) Rollback(
	ctx context.Context, spec application.ContainerSpec, req application.RollbackRequest, onEvent func(application.EngineEvent),
) (application.AttemptResult, error) {
	params := protocol.PeticionDeRollback{
		Version: protocol.LanguageVersion, Despliegue: req.DeploymentID, Solicitante: req.Requester,
		Metadatos: toMetadata(req.Project),
	}
	result, err := invoke[protocol.Resultado](ctx, d, spec, protocol.MethodRollback, params, req.Secrets, progress(onEvent))
	if err != nil {
		return application.AttemptResult{}, err
	}
	return toAttemptResult(result), nil
}

// --- HistoryReader ---

func (d *DockerEngine) Attempts(ctx context.Context, spec application.ContainerSpec, environment string) ([]application.AttemptSummary, error) {
	list, err := invoke[[]protocol.ResumenDeIntento](
		ctx, d, spec, protocol.MethodIntentos, byEnvironment(environment), nil, nil)
	if err != nil {
		return nil, err
	}
	return toSummaries(list), nil
}

func (d *DockerEngine) AttemptDetail(ctx context.Context, spec application.ContainerSpec, attemptID string) (application.AttemptDetail, error) {
	params := protocol.PeticionPorIntento{Version: protocol.LanguageVersion, Intento: attemptID}
	detail, err := invoke[protocol.IntentoDeHistorial](ctx, d, spec, protocol.MethodIntento, params, nil, nil)
	if err != nil {
		return application.AttemptDetail{}, err
	}
	return toAttemptDetail(detail), nil
}

func (d *DockerEngine) Logs(ctx context.Context, spec application.ContainerSpec, req application.LogsRequest) ([]application.CommandOutput, error) {
	logs, err := invoke[protocol.Logs](ctx, d, spec, protocol.MethodLogs, toLogsParams(req), nil, nil)
	if err != nil {
		return nil, err
	}
	return toCommandOutputs(logs), nil
}

func (d *DockerEngine) Deployments(ctx context.Context, spec application.ContainerSpec, environment string) ([]application.Deployment, error) {
	list, err := invoke[[]protocol.Despliegue](ctx, d, spec, protocol.MethodDespliegues, byEnvironment(environment), nil, nil)
	if err != nil {
		return nil, err
	}
	return toDeployments(list), nil
}

func (d *DockerEngine) Releases(ctx context.Context, spec application.ContainerSpec, environment string) ([]application.Release, error) {
	list, err := invoke[[]protocol.Lanzamiento](ctx, d, spec, protocol.MethodLanzamientos, byEnvironment(environment), nil, nil)
	if err != nil {
		return nil, err
	}
	return toReleases(list), nil
}

// --- PipelineInspector ---

func (d *DockerEngine) Check(ctx context.Context, spec application.ContainerSpec, req application.CheckRequest) (application.CheckResult, error) {
	result, err := invoke[protocol.ResultadoDeSimulacion](ctx, d, spec, protocol.MethodSimular, toCheckParams(req), nil, nil)
	if err != nil {
		return application.CheckResult{}, err
	}
	return toCheckResult(result), nil
}

func (d *DockerEngine) Environments(ctx context.Context, spec application.ContainerSpec, pipeline application.PipelineRef) ([]application.Environment, error) {
	list, err := invoke[[]protocol.Ambiente](ctx, d, spec, protocol.MethodAmbientes, toCatalogParams(pipeline), nil, nil)
	if err != nil {
		return nil, err
	}
	return toEnvironments(list), nil
}

func (d *DockerEngine) Steps(ctx context.Context, spec application.ContainerSpec, pipeline application.PipelineRef) ([]application.PipelineStep, error) {
	list, err := invoke[[]protocol.PasoDelPipeline](ctx, d, spec, protocol.MethodPasos, toCatalogParams(pipeline), nil, nil)
	if err != nil {
		return nil, err
	}
	return toPipelineSteps(list), nil
}

// --- ReleaseManager ---

func (d *DockerEngine) Release(ctx context.Context, spec application.ContainerSpec, req application.ReleaseRequest) (application.Release, error) {
	params := protocol.PeticionDeLanzar{
		Version: protocol.LanguageVersion, Ambiente: req.Environment, Despliegue: req.DeploymentID, Nombre: req.Name,
	}
	release, err := invoke[protocol.Lanzamiento](ctx, d, spec, protocol.MethodLanzar, params, nil, nil)
	if err != nil {
		return application.Release{}, err
	}
	return toRelease(release), nil
}

func (d *DockerEngine) Protect(ctx context.Context, spec application.ContainerSpec, environment string) error {
	_, err := invoke[struct{}](ctx, d, spec, protocol.MethodReservar, byEnvironment(environment), nil, nil)
	return err
}

func (d *DockerEngine) Unprotect(ctx context.Context, spec application.ContainerSpec, environment string) error {
	_, err := invoke[struct{}](ctx, d, spec, protocol.MethodLiberar, byEnvironment(environment), nil, nil)
	return err
}

// --- Diagnoser y AttemptAbandoner ---

func (d *DockerEngine) Diagnose(ctx context.Context, spec application.ContainerSpec, req application.DiagnoseRequest) (application.Diagnosis, error) {
	params := protocol.PeticionDeDiagnostico{
		Version: protocol.LanguageVersion, Intento: req.AttemptID, Referencia: req.ReferenceDeploymentID,
	}
	answer, err := invoke[protocol.RespuestaDeDiagnostico](ctx, d, spec, protocol.MethodDiagnosticar, params, nil, nil)
	if err != nil {
		return application.Diagnosis{}, err
	}
	return toDiagnosis(answer), nil
}

func (d *DockerEngine) Abandon(ctx context.Context, spec application.ContainerSpec, attemptID string) error {
	params := protocol.PeticionPorIntento{Version: protocol.LanguageVersion, Intento: attemptID}
	_, err := invoke[struct{}](ctx, d, spec, protocol.MethodAbandonar, params, nil, nil)
	return err
}

func byEnvironment(environment string) protocol.PeticionPorAmbiente {
	return protocol.PeticionPorAmbiente{Version: protocol.LanguageVersion, Ambiente: environment}
}

func toCatalogParams(p application.PipelineRef) protocol.PeticionDeCatalogo {
	return protocol.PeticionDeCatalogo{Version: protocol.LanguageVersion, FuenteDelPipeline: p.Source, Commit: p.Commit}
}
