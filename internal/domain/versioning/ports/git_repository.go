package ports

import (
	"context"

	"github.com/jairoprogramador/vex/internal/domain/versioning/vos"
)

// GitRepository define la interfaz para interactuar con un repositorio Git.
type GitRepository interface {
	// GetLastCommit obtiene el último commit de la rama actual.
	GetLastCommit(ctx context.Context, repoPath string) (*vos.Commit, error)

	// GetCommitsSinceTag obtiene la lista de commits desde el último tag semántico.
	GetCommitsSinceTag(ctx context.Context, repoPath string, lastTag string) ([]*vos.Commit, error)

	// GetLastSemverTag obtiene el último tag que sigue el formato de versionado semántico.
	GetLastSemverTag(ctx context.Context, repoPath string) (string, error)

	// CreateTagForCommit crea un nuevo tag apuntando a un commit específico.
	CreateTagForCommit(ctx context.Context, repoPath string, commitHash string, tagName string) error
}
