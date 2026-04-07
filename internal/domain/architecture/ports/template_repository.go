package ports

import (
	"github.com/jairoprogramador/vex/internal/domain/architecture/vos"
)

type TemplateRepository interface {
	GetExecutionUnit(query vos.QueryTemplate) (vos.ExecutionUnit, error)
}
