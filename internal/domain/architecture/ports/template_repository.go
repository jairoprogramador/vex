package ports

import (
	"github.com/jairoprogramador/vex-client/internal/domain/architecture/vos"
)

type TemplateRepository interface {
	GetExecutionUnit(query vos.QueryTemplate) (vos.ExecutionUnit, error)
}
