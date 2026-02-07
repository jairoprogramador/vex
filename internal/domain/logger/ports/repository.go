package ports

import (
	"github.com/jairoprogramador/vex/internal/application/dto"
	"github.com/jairoprogramador/vex/internal/domain/logger/aggregates"
)

type LoggerRepository interface {
	Save(namesParams dto.NamesParams, logger *aggregates.Logger) error
	Find(namesParams dto.NamesParams) (aggregates.Logger, error)
}
