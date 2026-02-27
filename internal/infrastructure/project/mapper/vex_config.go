package mapper

import (
	comVos "github.com/jairoprogramador/vex-client/internal/domain/common/vos"
	"github.com/jairoprogramador/vex-client/internal/domain/project/aggregates"
	proVos "github.com/jairoprogramador/vex-client/internal/domain/project/vos"
	"github.com/jairoprogramador/vex-client/internal/infrastructure/project/dto"
)

func ToDomainProject(configDto dto.ProjectDTO) (proVos.ProjectID, proVos.ProjectData, error) {
	id, err := proVos.NewProjectID(configDto.ID)
	if err != nil {
		return proVos.ProjectID{}, proVos.ProjectData{}, err
	}
	data, err := proVos.NewProjectData(
		configDto.Name,
		configDto.Organization,
		configDto.Team,
		configDto.Description)

	if err != nil {
		return proVos.ProjectID{}, proVos.ProjectData{}, err
	}
	return id, data, nil
}

func ToDomainRuntime(configDto dto.RuntimeDTO) (proVos.Runtime, error) {
	image, err := comVos.NewImage(
		configDto.Image,
		configDto.Tag)
	if err != nil {
		return proVos.Runtime{}, err
	}

	volumes := make([]proVos.Volume, 0, len(configDto.Run.Volumes))
	for _, dtoVol := range configDto.Run.Volumes {
		volume, err := proVos.NewVolume(dtoVol.Host, dtoVol.Container)
		if err != nil {
			return proVos.Runtime{}, err
		}
		volumes = append(volumes, volume)
	}

	envVars := make([]proVos.EnvVar, 0, len(configDto.Run.Env))
	for _, dtoEnv := range configDto.Run.Env {
		envVar, err := proVos.NewEnvVar(dtoEnv.Name, dtoEnv.Value)
		if err != nil {
			return proVos.Runtime{}, err
		}
		envVars = append(envVars, envVar)
	}

	args := make([]proVos.Argument, 0, len(configDto.Build.Args))
	for _, dtoArg := range configDto.Build.Args {
		arg, err := proVos.NewArgument(dtoArg.Name, dtoArg.Value)
		if err != nil {
			return proVos.Runtime{}, err
		}
		args = append(args, arg)
	}

	runtime := proVos.NewRuntime(
		proVos.WithImage(image),
		proVos.WithVolumes(volumes),
		proVos.WithEnv(envVars),
		proVos.WithArgs(args),
	)
	return runtime, nil
}

func ToDomain(configDto dto.FDConfigDTO) (*aggregates.Project, error) {

	id, data, err := ToDomainProject(configDto.Project)
	if err != nil {
		return nil, err
	}

	template, err := comVos.NewTemplate(configDto.Template.URL, configDto.Template.Ref)
	if err != nil {
		return nil, err
	}

	runtime, err := ToDomainRuntime(configDto.Runtime)
	if err != nil {
		return nil, err
	}

	return aggregates.NewProject(id, data, template, runtime)
}

func ToRuntimeDto(runtime proVos.Runtime) dto.RuntimeDTO {
	volumes := make([]dto.VolumeDTO, 0, len(runtime.Volumes()))
	for _, volume := range runtime.Volumes() {
		volumes = append(volumes, dto.VolumeDTO{
			Host:      volume.Host(),
			Container: volume.Container(),
		})
	}

	envVars := make([]dto.EnvVarDTO, 0, len(runtime.Env()))
	for _, envVar := range runtime.Env() {
		envVars = append(envVars, dto.EnvVarDTO{
			Name:  envVar.Name(),
			Value: envVar.Value(),
		})
	}

	args := make([]dto.BuildArgDTO, 0, len(runtime.Args()))
	for _, arg := range runtime.Args() {
		args = append(args, dto.BuildArgDTO{
			Name:  arg.Name(),
			Value: arg.Value(),
		})
	}

	return dto.RuntimeDTO{
		Image: runtime.Image().Image(),
		Tag:   runtime.Image().Tag(),
		Build: dto.BuildDTO{
			Args: args,
		},
		Run: dto.RunDTO{
			Volumes: volumes,
			Env:     envVars,
		},
	}
}

func ToDto(config *aggregates.Project) dto.FDConfigDTO {

	projectDto := dto.ProjectDTO{
		ID:           config.ID().String(),
		Name:         config.Data().Name(),
		Team:         config.Data().Team(),
		Description:  config.Data().Description(),
		Organization: config.Data().Organization(),
	}

	templateDto := dto.TemplateDTO{
		URL: config.Template().URL(),
		Ref: config.Template().Ref(),
	}

	runtimeDto := ToRuntimeDto(config.Runtime())

	return dto.FDConfigDTO{
		Project:  projectDto,
		Template: templateDto,
		Runtime:  runtimeDto,
	}
}
