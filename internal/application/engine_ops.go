package application

import (
	"context"
	"time"
)

// Roles del motor. Cada caso de uso declara solo el que necesita (interfaces pequeñas); EngineClient los junta
// para el adaptador, que los implementa todos.

// AttemptRunner ejecuta pipelines: un intento nuevo o volver a un despliegue anterior.
type AttemptRunner interface {
	Attempt(ctx context.Context, spec ContainerSpec, req AttemptRequest, onEvent func(EngineEvent)) (AttemptResult, error)
	Rollback(ctx context.Context, spec ContainerSpec, req RollbackRequest, onEvent func(EngineEvent)) (AttemptResult, error)
}

// HistoryReader consulta lo ocurrido; nunca escribe.
type HistoryReader interface {
	Attempts(ctx context.Context, spec ContainerSpec, environment string) ([]AttemptSummary, error)
	AttemptDetail(ctx context.Context, spec ContainerSpec, attemptID string) (AttemptDetail, error)
	Logs(ctx context.Context, spec ContainerSpec, req LogsRequest) ([]CommandOutput, error)
	Deployments(ctx context.Context, spec ContainerSpec, environment string) ([]Deployment, error)
	Releases(ctx context.Context, spec ContainerSpec, environment string) ([]Release, error)
}

// PipelineInspector mira el pipeline sin ejecutarlo.
type PipelineInspector interface {
	Check(ctx context.Context, spec ContainerSpec, req CheckRequest) (CheckResult, error)
	Environments(ctx context.Context, spec ContainerSpec, pipeline PipelineRef) ([]Environment, error)
	Steps(ctx context.Context, spec ContainerSpec, pipeline PipelineRef) ([]PipelineStep, error)
}

// ReleaseManager decide qué despliegue está visible en un ambiente y si lo decide una persona.
type ReleaseManager interface {
	Release(ctx context.Context, spec ContainerSpec, req ReleaseRequest) (Release, error)
	Protect(ctx context.Context, spec ContainerSpec, environment string) error
	Unprotect(ctx context.Context, spec ContainerSpec, environment string) error
}

// Diagnoser explica un fallo.
type Diagnoser interface {
	Diagnose(ctx context.Context, spec ContainerSpec, req DiagnoseRequest) (Diagnosis, error)
}

// AttemptAbandoner libera un ambiente atascado.
type AttemptAbandoner interface {
	Abandon(ctx context.Context, spec ContainerSpec, attemptID string) error
}

// EngineClient es el motor completo.
type EngineClient interface {
	AttemptRunner
	HistoryReader
	PipelineInspector
	ReleaseManager
	Diagnoser
	AttemptAbandoner
}

// --- Intentos ---

// AttemptCause es por qué un intento terminó sin que un comando lo decidiera; vacía en el caso normal.
type AttemptCause string

const (
	// CauseError: algo impidió seguir sin que ningún comando fallara.
	CauseError AttemptCause = "error"
	// CauseInterrupted: el proceso del intento murió sin cerrarlo.
	CauseInterrupted AttemptCause = "interrupted"
)

// Estados de un paso dentro del detalle de un intento (además de los de EngineEvent).
const (
	// StepUnfinished: empezó y nunca terminó (el proceso murió o un error lo detuvo).
	StepUnfinished StepStatus = "unfinished"
	// StepPending: el intento no llegó a él.
	StepPending StepStatus = "pending"
)

// AttemptSummary es un intento en una lista.
type AttemptSummary struct {
	ID          string
	Environment string
	Requester   string
	UntilStep   string
	StartedAt   time.Time
	// Status está vacío si el intento no tiene desenlace (sigue en curso, murió sin cerrarse o se abandonó).
	Status AttemptStatus
	Cause  AttemptCause
	// Abandoned: alguien lo dio por perdido a mano. El motor solo lo dice en el detalle de un intento; en una
	// lista el CLI lo completa con lo que recuerda.
	Abandoned bool
}

// InProgress dice si el intento sigue sin terminar: sin desenlace y sin haberse abandonado.
func (s AttemptSummary) InProgress() bool { return s.Status == "" && !s.Abandoned }

// AttemptDetail es un intento con lo que pasó en cada paso.
type AttemptDetail struct {
	AttemptSummary
	// RolledBackTo es el despliegue al que volvió, si fue un rollback.
	RolledBackTo string
	// Steps son los pasos hasta UntilStep, en orden, con lo que pasó en cada uno.
	Steps []StepOutcome
	// Duration es lo que tardó desde que abrió hasta su último registro.
	Duration time.Duration
}

type StepOutcome struct {
	Name   string
	Status StepStatus
}

// --- Despliegues y lanzamientos ---

type Deployment struct {
	ID          string
	Environment string
	AttemptID   string
	ParentID    string
	At          time.Time
}

type Release struct {
	ID           string
	Environment  string
	DeploymentID string
	// Version crece por proyecto con cada código distinto que se lanza; no es consecutiva dentro de un ambiente.
	Version int
	Name    string
	At      time.Time
}

type ReleaseRequest struct {
	Environment  string
	DeploymentID string
	// Name es opcional: vacío toma el número de versión.
	Name string
}

type RollbackRequest struct {
	DeploymentID string
	Requester    string
	Project      ProjectMetadata
	// Secrets viajan por stdin hacia los comandos del pipeline, igual que en un intento.
	Secrets map[string]string
}

// --- Pipeline ---

// PipelineRef es el pipeline sobre el que se pregunta: su repositorio en el contenedor y, opcional, un commit.
type PipelineRef struct {
	Source string
	Commit string
}

type Environment struct {
	Name        string
	Description string
	// Value es con el que se nombra el ambiente en los comandos (`vex test sand`).
	Value string
	// Protected: el dueño decide los lanzamientos; el motor no lanza solo.
	Protected bool
}

type PipelineStep struct {
	Name   string
	Order  int
	Shared bool
}

type CheckRequest struct {
	Environment string
	Requester   string
	UntilStep   string
	Pipeline    PipelineRef
	Project     ProjectMetadata
}

// CheckResult: Valid, o los fallos del pipeline, o las variables que faltan; nunca los dos a la vez.
type CheckResult struct {
	Valid    bool
	Failures []PipelineFailure
	Missing  []MissingVariables
}

type MissingVariables struct {
	Step      string
	Variables []string
}

// --- Diagnóstico ---

type DiagnoseRequest struct {
	AttemptID string
	// ReferenceDeploymentID es opcional: con qué despliegue comparar; vacío usa el último anterior del ambiente.
	ReferenceDeploymentID string
}

type DiagnosisKind string

const (
	// DiagnosisFound: hay una comparación.
	DiagnosisFound DiagnosisKind = "found"
	// DiagnosisNoReference: no hay un despliegue anterior con el que comparar.
	DiagnosisNoReference DiagnosisKind = "no_reference"
	// DiagnosisNotAttributable: cancelado, interrumpido o en curso: no falló por un cambio.
	DiagnosisNotAttributable DiagnosisKind = "not_attributable"
)

type Diagnosis struct {
	Kind        DiagnosisKind
	Environment string
	// Reference es la última vez que funcionó; Failed, el intento que falla.
	Reference DiagnosedAttempt
	Failed    DiagnosedAttempt
	// AttemptsSince es cuántos intentos hubo desde la referencia; 0 si se eligió la referencia a mano.
	AttemptsSince int
	Changes       DiagnosisChanges
}

type DiagnosedAttempt struct {
	ID string
	At time.Time
}

// DiagnosisChanges dice qué cambió; un eje vacío es que no cambió.
type DiagnosisChanges struct {
	Code         []string
	Instructions []string
	DeclaredVars []VariableRef
	ProducedVars []VariableRef
}

// Nothing dice si no cambió nada de lo que mira cada paso.
func (c DiagnosisChanges) Nothing() bool {
	return len(c.Code) == 0 && len(c.Instructions) == 0 && len(c.DeclaredVars) == 0 && len(c.ProducedVars) == 0
}

type VariableRef struct {
	Step string
	Name string
}
