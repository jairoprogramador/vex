package ports

import (
	"context"
	"github.com/jairoprogramador/vex/internal/domain/versioning/vos"
)

// VersionCalculator define la interfaz para calcular la siguiente versión.
type VersionCalculator interface {
	CalculateNextVersion(ctx context.Context, repoPath string, forceDateVersion bool) (*vos.Version, *vos.Commit, error)
}
