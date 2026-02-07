package mapper

import (
	"github.com/jairoprogramador/vex/internal/domain/logger/vos"
	"github.com/jairoprogramador/vex/internal/infrastructure/logger/dto"
)

func OutputToDTO(output *vos.OutputLine) dto.OutputDTO {
	return dto.OutputDTO{
		Timestamp: output.Timestamp(),
		Line:      output.Line(),
	}
}

func OutputToDomain(outputDTO dto.OutputDTO) vos.OutputLine {
	return vos.HydrateOutputLine(
		outputDTO.Timestamp,
		outputDTO.Line,
	)
}
