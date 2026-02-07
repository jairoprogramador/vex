package aggregates

import (
	"errors"

	"github.com/jairoprogramador/vex/internal/domain/definition/entities"
	"github.com/jairoprogramador/vex/internal/domain/definition/vos"
)

type ExecutionPlanDefinition struct {
	environment vos.EnvironmentDefinition
	steps       []*entities.StepDefinition
}

func NewExecutionPlanDefinition(
	env vos.EnvironmentDefinition,
	steps []*entities.StepDefinition) (*ExecutionPlanDefinition, error) {

	if len(steps) == 0 {
		return nil, errors.New("el plan de ejecución debe contener al menos un paso")
	}
	return &ExecutionPlanDefinition{
		environment: env,
		steps:       steps,
	}, nil
}

func (p *ExecutionPlanDefinition) Environment() vos.EnvironmentDefinition {
	return p.environment
}

func (p *ExecutionPlanDefinition) Steps() []*entities.StepDefinition {
	return p.steps
}
