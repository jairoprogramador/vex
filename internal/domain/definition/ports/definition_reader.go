package ports

import (
	"context"

	"github.com/jairoprogramador/vex/internal/domain/definition/vos"
)

type DefinitionReader interface {
	ReadEnvironments(ctx context.Context, sourcePath string) ([]vos.EnvironmentDefinition, error)
	ReadStepNames(ctx context.Context, stepsDir string) ([]vos.StepNameDefinition, error)
	ReadCommands(ctx context.Context, commandsFilePath string) ([]vos.CommandDefinition, error)
	ReadVariables(ctx context.Context, variablesFilePath string) ([]vos.VariableDefinition, error)
}
