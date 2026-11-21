package application

import (
	"fmt"
	"regexp"
	"time"
)

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
	// Context identifica algo externo de lo que depende lo que los pasos hacen (la conexión de nube y su
	// destino). El motor lo mezcla en la huella de variables: si cambia, no reutiliza pasos de otro contexto.
	// Opaco y sin secretos; vacío si no aplica.
	Context string
}

type ProjectMetadata struct {
	ID           string
	Name         string
	Organization string
	Team         string
}

// engineNamePattern es lo único que el motor admite en el ambiente y en los metadatos del proyecto: acaban tal
// cual en la línea de los comandos del pipeline.
var engineNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// InvalidEngineNameError: un dato que el motor pondría en la línea de un comando no tiene la forma que admite.
// No lleva el valor: basta con decir dónde corregirlo.
type InvalidEngineNameError struct {
	// What es el dato en palabras ("el nombre del proyecto") y Where, dónde se corrige ("project.name en
	// vexconfig.yaml"); Where vacío es el ambiente que se tecleó.
	What  string
	Where string
}

func (e *InvalidEngineNameError) Error() string {
	const rule = "solo admite letras sin acentos, números, punto, guion y guion bajo, sin espacios, empezando por " +
		"letra o número (128 como mucho)"
	if e.Where == "" {
		return fmt.Sprintf("%s no es válido: %s.", e.What, rule)
	}
	return fmt.Sprintf("%s no es válido: %s.\n→ Corrige %s", e.What, rule, e.Where)
}

// engineFields relaciona cada campo que el motor puede rechazar con el dato de vexconfig.yaml del que sale.
var engineFields = map[string]InvalidEngineNameError{
	"Metadatos.ProjectId":           {What: "El id del proyecto", Where: "project.id en vexconfig.yaml"},
	"Metadatos.ProjectName":         {What: "El nombre del proyecto", Where: "project.name en vexconfig.yaml"},
	"Metadatos.ProjectOrganization": {What: "La organización del proyecto", Where: "project.organization en vexconfig.yaml"},
	"Metadatos.ProjectTeam":         {What: "El equipo del proyecto", Where: "project.team en vexconfig.yaml"},
}

// InvalidEngineField dice qué dato de vexconfig.yaml corresponde a un campo de metadatos rechazado por el motor.
func InvalidEngineField(field string) (*InvalidEngineNameError, bool) {
	known, ok := engineFields[field]
	return &known, ok
}

// Validate comprueba lo mismo que el motor, antes de lanzar nada. Un metadato vacío vale.
func (m ProjectMetadata) Validate() error {
	values := []struct{ field, value string }{
		{"Metadatos.ProjectId", m.ID}, {"Metadatos.ProjectName", m.Name},
		{"Metadatos.ProjectOrganization", m.Organization}, {"Metadatos.ProjectTeam", m.Team},
	}
	for _, v := range values {
		if v.value != "" && !engineNamePattern.MatchString(v.value) {
			invalid, _ := InvalidEngineField(v.field)
			return invalid
		}
	}
	return nil
}

// ValidateEngineEnvironment comprueba que el ambiente tecleado tiene la forma que el motor admite.
func ValidateEngineEnvironment(name string) error {
	if !engineNamePattern.MatchString(name) {
		return &InvalidEngineNameError{What: "El nombre del ambiente"}
	}
	return nil
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
	// Visible son las variables que el pipeline marcó para mostrar (la URL de la app, por ejemplo), con su
	// valor. Solo vienen de pasos que salieron bien; vacío si el motor de la imagen es anterior a ellas.
	Visible []VisibleOutput
	// Elapsed es lo que tardó toda la ejecución vista desde la CLI, preparación incluida. Lo pone quien
	// ejecuta, no el motor (Duration es solo el intento).
	Elapsed time.Duration
}

// VisibleOutput es una variable que el pipeline pide mostrar a quien ejecuta.
type VisibleOutput struct {
	Name        string
	Description string
	Value       string
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
	// EngineContextUnsupported: la imagen del motor es anterior al contexto de ejecución (AttemptRequest.Context).
	EngineContextUnsupported EngineErrorKind = "context_unsupported"
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
