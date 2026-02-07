package ports

import "context"
import "github.com/jairoprogramador/vex/internal/domain/execution/vos"

type CommandRunner interface {
	Run(ctx context.Context, command string, workDir string) (*vos.CommandResult, error)
}
