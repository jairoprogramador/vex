package ports

import (
	"github.com/jairoprogramador/vex-client/internal/domain/architecture/vos"
)

type TemplateRepository interface {
	GetTemplates(level vos.Level, response []int) (string, error)
}
