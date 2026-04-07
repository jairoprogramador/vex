package ports

import (
	"github.com/jairoprogramador/vex/internal/domain/docker/vos"
	proAgg "github.com/jairoprogramador/vex/internal/domain/project/aggregates"
)

// ImageService define el contrato para la lógica de construcción de opciones de imagen.
type ImageService interface {
	CreateOptions(project *proAgg.Project) (vos.BuildOptions, error)
	BuildCommand(opts vos.BuildOptions) (string, error)
}
