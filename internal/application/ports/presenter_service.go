package ports

import (
	"github.com/jairoprogramador/vex/internal/domain/logger/aggregates"
	"github.com/jairoprogramador/vex/internal/domain/logger/entities"
)

type PresenterService interface {
	Line()
	Header(log *aggregates.Logger, revision string)
	Step(step *entities.StepRecord)
	Task(task *entities.TaskRecord, step *entities.StepRecord)
	FinalSummary(log *aggregates.Logger)
}
