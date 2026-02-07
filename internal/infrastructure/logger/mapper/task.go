package mapper

import (
	"github.com/jairoprogramador/vex/internal/domain/logger/entities"
	"github.com/jairoprogramador/vex/internal/domain/logger/vos"
	"github.com/jairoprogramador/vex/internal/infrastructure/logger/dto"
)

func TaskToDTO(task *entities.TaskRecord) dto.TaskDTO {
	outputs := make([]dto.OutputDTO, 0, len(task.Output()))
	for _, output := range task.Output() {
		outputs = append(outputs, OutputToDTO(&output))
	}
	return dto.TaskDTO{
		Name:      task.Name(),
		Status:    task.Status().String(),
		Command:   task.Command(),
		StartTime: task.StartTime(),
		EndTime:   task.EndTime(),
		Output:    outputs,
		Err:       task.Error(),
	}
}

func TaskToDomain(tasksDTO dto.TaskDTO) (*entities.TaskRecord, error) {
	outputs := make([]vos.OutputLine, 0, len(tasksDTO.Output))
	for _, output := range tasksDTO.Output {
		outputs = append(outputs, OutputToDomain(output))
	}

	status, err := vos.NewStatusFromString(tasksDTO.Status)
	if err != nil {
		return nil, err
	}

	return entities.HydrateTaskRecord(
		tasksDTO.Name,
		status,
		tasksDTO.Command,
		tasksDTO.StartTime,
		tasksDTO.EndTime,
		outputs,
		tasksDTO.Err,
	)
}
