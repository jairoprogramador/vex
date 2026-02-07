package ports

import (
	"context"

	"github.com/jairoprogramador/vex/internal/domain/execution/entities"
	"github.com/jairoprogramador/vex/internal/domain/execution/vos"
)

// StepExecutor define la interfaz para ejecutar un único paso de un plan de ejecución.
type StepExecutor interface {
	Execute(ctx context.Context, step *entities.Step, initialVars vos.VariableSet) (*vos.ExecutionResult, error)
}
