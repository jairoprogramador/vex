package ports

import "github.com/jairoprogramador/vex/internal/domain/project/aggregates"

type ProjectRepository interface {
	Save(project *aggregates.Project) error
	Exists() (bool, error)
	Load() (*aggregates.Project, error)
}
