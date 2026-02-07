package mapper

import (
	"github.com/jairoprogramador/vex/internal/domain/logger/aggregates"
	"github.com/jairoprogramador/vex/internal/domain/logger/entities"
	"github.com/jairoprogramador/vex/internal/domain/logger/vos"
	"github.com/jairoprogramador/vex/internal/infrastructure/logger/dto"
)

func LoggerToDTO(logger *aggregates.Logger) dto.LoggerDTO {
	steps := make([]dto.StepDTO, 0, len(logger.Steps()))
	for _, step := range logger.Steps() {
		steps = append(steps, StepToDTO(step))
	}
	return dto.LoggerDTO{
		Status:    logger.Status().String(),
		StartTime: logger.StartTime(),
		EndTime:   logger.EndTime(),
		Steps:     steps,
		Context:   logger.Context(),
		Revision:  logger.Revision(),
	}
}

func LoggerToDomain(loggerDTO dto.LoggerDTO) (*aggregates.Logger, error) {
	steps := make([]*entities.StepRecord, 0, len(loggerDTO.Steps))
	for _, stepDTO := range loggerDTO.Steps {
		step, err := StepToDomain(stepDTO)
		if err != nil {
			return nil, err
		}
		steps = append(steps, step)
	}

	status, err := vos.NewStatusFromString(loggerDTO.Status)
	if err != nil {
		return nil, err
	}
	return aggregates.HydrateLogger(
		status,
		loggerDTO.StartTime,
		loggerDTO.EndTime,
		steps,
		loggerDTO.Context,
		loggerDTO.Revision)
}
