package application

import (
	"context"
	"errors"
	"fmt"
)

// UnknownStepError: el paso que se tecleó no existe en el pipeline.
type UnknownStepError struct {
	Name       string
	Known      []string
	Suggestion string
}

func (e *UnknownStepError) Error() string {
	return fmt.Sprintf("el paso %q no existe en el pipeline", e.Name)
}

// CheckFailedError: el pipeline no está listo para ejecutarse. Trae lo que el motor encontró: fallos del propio
// pipeline, o variables que un paso usa y no se resuelven. Nunca las dos a la vez.
type CheckFailedError struct {
	Environment string
	UntilStep   string
	Failures    []PipelineFailure
	Missing     []MissingVariables
}

func (e *CheckFailedError) Error() string {
	return fmt.Sprintf("el pipeline no está listo para ejecutar %q en %q", e.UntilStep, e.Environment)
}

// pipelineEngine es lo que el guardián necesita del motor.
type pipelineEngine interface {
	Check(ctx context.Context, spec ContainerSpec, req CheckRequest) (CheckResult, error)
	Environments(ctx context.Context, spec ContainerSpec, pipeline PipelineRef) ([]Environment, error)
	Steps(ctx context.Context, spec ContainerSpec, pipeline PipelineRef) ([]PipelineStep, error)
}

// PipelineGuard comprueba, sin ejecutar nada, que un paso se puede ejecutar en un ambiente, y traduce lo que
// encuentre a errores que una persona entiende. Lo usan `vex check` y el pre-vuelo de `vex <step> <environment>`.
//
// Cuando el motor dice que un ambiente o un paso no existe, el guardián le pregunta al propio motor cuáles hay:
// así el mensaje trae los nombres válidos y una sugerencia, sin depender de ninguna copia que pueda estar vieja.
type PipelineGuard struct {
	engine  pipelineEngine
	recents *RecentsService
}

func NewPipelineGuard(engine pipelineEngine, recents *RecentsService) *PipelineGuard {
	return &PipelineGuard{engine: engine, recents: recents}
}

// Check devuelve nil si todo está en orden. Si no: *CheckFailedError, *UnknownEnvironmentError,
// *UnknownStepError, o el error del motor tal cual si no es algo que sepa explicar.
func (g *PipelineGuard) Check(ctx context.Context, spec ContainerSpec, projectID string, req CheckRequest) error {
	result, err := g.engine.Check(ctx, spec, req)
	if err != nil {
		return g.Explain(ctx, spec, projectID, req.Pipeline, err)
	}
	if result.Valid {
		return nil
	}
	return &CheckFailedError{
		Environment: req.Environment, UntilStep: req.UntilStep, Failures: result.Failures, Missing: result.Missing,
	}
}

// Explain convierte un «parámetro inválido» sobre el ambiente o el paso, venga de la operación que venga
// (simular, intentar…), en el error que dice cuáles existen. Cualquier otro error pasa tal cual. Permite dar el
// mensaje amigable cuando algo falla sin tener que comprobar antes en cada ejecución.
func (g *PipelineGuard) Explain(ctx context.Context, spec ContainerSpec, projectID string, pipeline PipelineRef, err error) error {
	var engineErr *EngineError
	if !errors.As(err, &engineErr) || engineErr.Kind != EngineInvalidParams {
		return err
	}
	switch engineErr.Field {
	case "Ambiente":
		return g.explainEnvironment(ctx, spec, projectID, pipeline, engineErr, err)
	case "HastaPaso":
		return g.explainStep(ctx, spec, projectID, pipeline, engineErr, err)
	}
	return err
}

func (g *PipelineGuard) explainEnvironment(
	ctx context.Context, spec ContainerSpec, projectID string, pipeline PipelineRef, engineErr *EngineError, original error,
) error {
	environments, err := g.engine.Environments(ctx, spec, pipeline)
	if err != nil {
		return original // sin el catálogo no se puede explicar mejor: se muestra lo que dijo el motor
	}
	_ = g.recents.RememberEnvironments(projectID, environments)
	known := make([]string, len(environments))
	for i, e := range environments {
		known[i] = e.Value
	}
	suggestion, _ := ClosestName(engineErr.Value, known)
	return &UnknownEnvironmentError{Name: engineErr.Value, Known: known, Suggestion: suggestion}
}

func (g *PipelineGuard) explainStep(
	ctx context.Context, spec ContainerSpec, projectID string, pipeline PipelineRef, engineErr *EngineError, original error,
) error {
	steps, err := g.engine.Steps(ctx, spec, pipeline)
	if err != nil {
		return original
	}
	_ = g.recents.RememberSteps(projectID, steps)
	known := make([]string, len(steps))
	for i, s := range steps {
		known[i] = s.Name
	}
	suggestion, _ := ClosestName(engineErr.Value, known)
	return &UnknownStepError{Name: engineErr.Value, Known: known, Suggestion: suggestion}
}
