package engine

import (
	"errors"

	"github.com/jairoprogramador/vex/internal/application"
	"github.com/jairoprogramador/vex/internal/infrastructure/engine/protocol"
)

// Este archivo es la capa anticorrupción: traduce entre el lenguaje del motor
// (español, PascalCase) y el de la aplicación.

func toAttemptParams(req application.AttemptRequest) protocol.PeticionDeIntento {
	return protocol.PeticionDeIntento{
		Version:           protocol.LanguageVersion,
		Ambiente:          req.Environment,
		Solicitante:       req.Requester,
		FuenteDelProyecto: req.ProjectSource,
		CommitDelProyecto: req.ProjectCommit,
		FuenteDelPipeline: req.PipelineSource,
		CommitDelPipeline: req.PipelineCommit,
		HastaPaso:         req.UntilStep,
		Metadatos: protocol.Metadatos{
			ProjectId:           req.Project.ID,
			ProjectName:         req.Project.Name,
			ProjectOrganization: req.Project.Organization,
			ProjectTeam:         req.Project.Team,
		},
	}
}

func toLogsParams(req application.LogsRequest) protocol.PeticionDeLogs {
	params := protocol.PeticionDeLogs{Version: protocol.LanguageVersion, Intento: req.AttemptID}
	if req.OnlyFailed {
		params.Resultado = protocol.EstadoFallido
	}
	return params
}

func toAttemptResult(r protocol.Resultado) application.AttemptResult {
	steps := make([]application.StepResult, len(r.Detalle.Pasos))
	for i, p := range r.Detalle.Pasos {
		steps[i] = application.StepResult{Name: p.Nombre, Status: toStepStatus(p.Estado)}
	}
	return application.AttemptResult{
		AttemptID:    r.Intento,
		Status:       toAttemptStatus(r.Estado),
		DeploymentID: r.Despliegue,
		Duration:     r.Detalle.Tiempo,
		Steps:        steps,
	}
}

func toAttemptStatus(s string) application.AttemptStatus {
	switch s {
	case protocol.EstadoExitoso:
		return application.AttemptSucceeded
	case protocol.EstadoFallido:
		return application.AttemptFailed
	case protocol.EstadoCancelado:
		return application.AttemptCanceled
	}
	return application.AttemptStatus(s)
}

func toStepStatus(s string) application.StepStatus {
	switch s {
	case protocol.PasoEjecutado:
		return application.StepExecuted
	case protocol.PasoPrecargado:
		return application.StepReused
	case protocol.PasoFallido:
		return application.StepFailed
	case protocol.PasoCancelado:
		return application.StepCanceled
	}
	return application.StepStatus(s)
}

// toEngineEvent traduce un "progreso". Los eventos o notificaciones que esta
// versión de la CLI no conoce se ignoran: el motor puede añadir eventos nuevos.
func toEngineEvent(msg *protocol.Message) (application.EngineEvent, bool) {
	p, err := protocol.DecodeProgress(msg)
	if err != nil {
		return application.EngineEvent{}, false
	}
	event := application.EngineEvent{AttemptID: p.Intento, Step: p.Paso, Command: p.Comando}
	switch p.Evento {
	case protocol.EventoIntentoIniciado:
		event.Kind = application.AttemptStarted
	case protocol.EventoPasoIniciado:
		event.Kind = application.StepStarted
	case protocol.EventoComandoTerminado:
		event.Kind = application.CommandFinished
		event.Succeeded = p.Estado == protocol.EstadoExitoso
	case protocol.EventoPasoTerminado:
		event.Kind = application.StepFinished
		event.StepStatus = toStepStatus(p.Estado)
	default:
		return application.EngineEvent{}, false
	}
	return event, true
}

func toCommandOutputs(logs protocol.Logs) []application.CommandOutput {
	outputs := make([]application.CommandOutput, len(logs.Salidas))
	for i, s := range logs.Salidas {
		outputs[i] = application.CommandOutput{Step: s.Paso, Command: s.Comando, Succeeded: s.Exitoso, Text: s.Texto}
	}
	return outputs
}

var errorKinds = map[protocol.ErrorKind]application.EngineErrorKind{
	protocol.ErrAmbienteOcupado:       application.EngineEnvironmentBusy,
	protocol.ErrRechazado:             application.EnginePipelineRejected,
	protocol.ErrVersionNoSoportada:    application.EngineUnsupported,
	protocol.ErrParametrosInvalidos:   application.EngineInvalidParams,
	protocol.ErrNoExiste:              application.EngineNotFound,
	protocol.ErrNoDisponible:          application.EngineUnavailable,
	protocol.ErrConfiguracionInvalida: application.EngineInvalidConfig,
	protocol.ErrCancelado:             application.EngineCanceled,
	protocol.ErrEscrituraConcurrente:  application.EngineConcurrentWrite,
	protocol.ErrHistorialSinIntentos:  application.EngineNoAttempts,
	protocol.ErrInterno:               application.EngineInternal,
	protocol.ErrOperacionDesconocida:  application.EngineUnknownOperation,
}

// translateError convierte un error del motor en application.EngineError; otros
// errores (resultado ilegible) se devuelven tal cual.
func translateError(err error, stderr string) error {
	var engineErr *protocol.EngineError
	if !errors.As(err, &engineErr) {
		return err
	}
	kind, known := errorKinds[engineErr.Kind()]
	if !known {
		kind = application.EngineUnknown
	}
	translated := &application.EngineError{
		Kind:        kind,
		Message:     engineErr.Message,
		Environment: engineErr.Data.Ambiente,
		AttemptID:   engineErr.Data.Intento,
		Variable:    engineErr.Data.Variable,
		Field:       engineErr.Data.Campo,
		Value:       engineErr.Data.Valor,
	}
	for _, f := range engineErr.Data.Fallos {
		translated.Failures = append(translated.Failures, application.PipelineFailure{
			Invariant: f.Invariante, File: f.Fichero, Step: f.Paso, Detail: f.Detalle,
		})
	}
	if kind == application.EngineInternal {
		translated.Stderr = stderr
	}
	return translated
}
