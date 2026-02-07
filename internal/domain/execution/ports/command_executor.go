package ports

import (
	"context"

	"github.com/jairoprogramador/vex/internal/domain/execution/vos"
)

type CommandExecutor interface {
	Execute(
		ctx context.Context,
		command vos.Command,
		currentVars vos.VariableSet,
		workspaceStep, workspaceShared string) *vos.ExecutionResult
}
