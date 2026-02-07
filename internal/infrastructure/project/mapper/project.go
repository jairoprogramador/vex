package mapper

import (
	"github.com/jairoprogramador/vex/internal/domain/project/ports"
	"github.com/jairoprogramador/vex/internal/infrastructure/project/dto"
)

func ProjectToDto(data *ports.ProjectConfigDTO) dto.FdConfigDTO {
	return dto.FdConfigDTO{
		Project: dto.ProjectDTO{
			ID:           data.ID,
			Name:         data.Name,
			Organization: data.Organization,
			Team:         data.Team,
			Description:  data.Description,
			Version:      data.Version,
		},
		Template: dto.TemplateDTO{
			URL: data.TemplateURL,
			Ref: data.TemplateRef,
		},
	}
}

func ProjectToDomain(fdConfig dto.FdConfigDTO) *ports.ProjectConfigDTO {
	return &ports.ProjectConfigDTO{
		ID:           fdConfig.Project.ID,
		Name:         fdConfig.Project.Name,
		Organization: fdConfig.Project.Organization,
		Team:         fdConfig.Project.Team,
		Description:  fdConfig.Project.Description,
		Version:      fdConfig.Project.Version,
		TemplateURL:  fdConfig.Template.URL,
		TemplateRef:  fdConfig.Template.Ref,
	}
}
