package mapper

import (
	"errors"

	"github.com/jairoprogramador/vex/internal/domain/logger/entities"
	"github.com/jairoprogramador/vex/internal/domain/logger/vos"
	"github.com/jairoprogramador/vex/internal/infrastructure/logger/dto"
)

func StepToDTO(step *entities.StepRecord) dto.StepDTO {
	tasks := make([]dto.TaskDTO, 0, len(step.Tasks()))
	for _, task := range step.Tasks() {
		tasks = append(tasks, TaskToDTO(task))
	}
	errString := ""
	if step.Error() != nil {
		errString = step.Error().Error()
	}
	return dto.StepDTO{
		Name:      step.Name(),
		Status:    step.Status().String(),
		StartTime: step.StartTime(),
		EndTime:   step.EndTime(),
		Reason:    step.Reason(),
		Tasks:     tasks,
		Err:       errString,
	}
}

func StepToDomain(stepDTO dto.StepDTO) (*entities.StepRecord, error) {
	tasks := make([]*entities.TaskRecord, 0, len(stepDTO.Tasks))
	for _, taskDTO := range stepDTO.Tasks {
		task, err := TaskToDomain(taskDTO)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}

	status, err := vos.NewStatusFromString(stepDTO.Status)
	if err != nil {
		return nil, err
	}
	return entities.HydrateStepRecord(
		stepDTO.Name,
		status,
		stepDTO.StartTime,
		stepDTO.EndTime,
		stepDTO.Reason,
		tasks,
		errors.New(stepDTO.Err))
}
