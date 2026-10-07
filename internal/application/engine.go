package application

import "fmt"

// Los puertos hacia vex-engine están en engine_ops.go (por rol). Cada llamada ejecuta un contenedor
// descartable con el motor dentro: el motor atiende una sola petición por proceso.

// ContainerSpec describe el contenedor donde corre el motor. El orden de Mounts
// y Env se conserva tal cual para que la ejecución sea determinista.
type ContainerSpec struct {
	Image  string
	Mounts []Mount
	Env    []EnvVar
}

type Mount struct {
	Source   string
	Target   string
	ReadOnly bool
}

type EnvVar struct {
	Name  string
	Value string
}

type AttemptRequest struct {
	Environment    string
	Requester      string
	ProjectSource  string
	ProjectCommit  string
	PipelineSource string
	PipelineCommit string
	UntilStep      string
	Project        ProjectMetadata
	// Secrets viajan por stdin hacia los comandos del pipeline; nunca por argv ni por -e.
	Secrets map[string]string
}

type ProjectMetadata struct {
	ID           string
	Name         string
	Organization string
	Team         string
}

type AttemptStatus string

const (
	AttemptSucceeded AttemptStatus = "succeeded"
	AttemptFailed    AttemptStatus = "failed"
	AttemptCanceled  AttemptStatus = "canceled"
)

type StepStatus string

const (
	StepExecuted StepStatus = "executed"
	StepReused   StepStatus = "reused"
	StepFailed   StepStatus = "failed"
	StepCanceled StepStatus = "canceled"
)

type AttemptResult struct {
	AttemptID    string
	Status       AttemptStatus
	DeploymentID string
	Duration     string
	Steps        []StepResult
}

type StepResult struct {
	Name   string
	Status StepStatus
}

type EngineEventKind string

const (
	AttemptStarted  EngineEventKind = "attempt_started"
	StepStarted     EngineEventKind = "step_started"
	CommandFinished EngineEventKind = "command_finished"
	StepFinished    EngineEventKind = "step_finished"
)

// EngineEvent es el avance de un intento. No lleva la salida de los comandos:
// esa se consulta con EngineClient.Logs.
type EngineEvent struct {
	Kind      EngineEventKind
	AttemptID string
	Step      string
	Command   string
	// Succeeded solo aplica a CommandFinished.
	Succeeded bool
	// StepStatus solo aplica a StepFinished.
	StepStatus StepStatus
}

type LogsRequest struct {
	AttemptID  string
	OnlyFailed bool
}

type CommandOutput struct {
	Step      string
	Command   string
	Succeeded bool
	Text      string
}

// EngineErrorKind clasifica los errores del motor que la CLI sabe explicar.
type EngineErrorKind string

const (
	EngineEnvironmentBusy  EngineErrorKind = "environment_busy"
	EnginePipelineRejected EngineErrorKind = "pipeline_rejected"
	EngineUnsupported      EngineErrorKind = "unsupported_version"
	EngineInvalidParams    EngineErrorKind = "invalid_params"
	EngineNotFound         EngineErrorKind = "not_found"
	EngineUnavailable      EngineErrorKind = "unavailable"
	EngineInvalidConfig    EngineErrorKind = "invalid_config"
	EngineCanceled         EngineErrorKind = "canceled"
	EngineConcurrentWrite  EngineErrorKind = "concurrent_write"
	EngineNoAttempts       EngineErrorKind = "no_attempts"
	EngineInternal         EngineErrorKind = "internal"
	EngineUnknown          EngineErrorKind = "unknown"
	EngineDidNotRespond    EngineErrorKind = "did_not_respond"
	// EngineUnknownOperation: la imagen del motor es anterior a la operación pedida.
	EngineUnknownOperation EngineErrorKind = "unknown_operation"
)

// PipelineFailure es un incumplimiento de invariante del pipeline.
type PipelineFailure struct {
	Invariant string
	File      string
	Step      string
	Detail    string
}

// EngineError es un error del motor traducido a lenguaje de la aplicación.
type EngineError struct {
	Kind        EngineErrorKind
	Message     string
	Environment string
	AttemptID   string
	Variable    string
	// Field y Value son lo que no valía en una petición inválida: el parámetro (p. ej. "Ambiente") y lo que llegó.
	Field    string
	Value    string
	Failures []PipelineFailure
	// Stderr es la causa interna del contenedor, útil solo para errores internos.
	Stderr string
}

func (e *EngineError) Error() string {
	return fmt.Sprintf("motor: %s [%s]", e.Message, e.Kind)
}
