package application

import (
	"context"
	"errors"
	"fmt"

	comVos "github.com/jairoprogramador/vex-client/internal/domain/common/vos"
	docPor "github.com/jairoprogramador/vex-client/internal/domain/docker/ports"
	docVos "github.com/jairoprogramador/vex-client/internal/domain/docker/vos"
	proPor "github.com/jairoprogramador/vex-client/internal/domain/project/ports"
)

const MessageProjectNotInitialized = "project not initialized. Please run 'vex init' first"

type ExecutorService struct {
	projectRepository proPor.ProjectRepository
	commandExecutor   docPor.CommandExecutor
	imageService      docPor.ImageService
	containerService  docPor.ContainerService
}

func NewExecutorService(
	projectRepository proPor.ProjectRepository,
	commandExecutor docPor.CommandExecutor,
	imageService docPor.ImageService,
	containerService docPor.ContainerService,
) *ExecutorService {
	return &ExecutorService{
		projectRepository: projectRepository,
		commandExecutor:   commandExecutor,
		imageService:      imageService,
		containerService:  containerService,
	}
}

func (s *ExecutorService) Run(ctx context.Context, command, environment string) error {
	exists, err := s.projectRepository.Exists()
	if err != nil {
		return err
	}
	if !exists {
		return errors.New(MessageProjectNotInitialized)
	}

	project, err := s.projectRepository.Load()
	if err != nil {
		return err
	}

	var imageToUse docVos.ImageName

	imageInfo := project.Runtime().Image()

	if imageInfo.Image() == comVos.DefaultContainerImage {
		imageOptions, err := s.imageService.CreateOptions(project)
		if err != nil {
			return err
		}

		buildCommand, err := s.imageService.BuildCommand(imageOptions)
		if err != nil {
			return err
		}

		if _, err = s.commandExecutor.Execute(ctx, buildCommand); err != nil {
			return err
		}
		imageToUse = imageOptions.Image()
	} else {
		imageToUse, err = docVos.NewImageName(imageInfo.Image(), imageInfo.Tag())
		if err != nil {
			return err
		}
	}

	commandVex := fmt.Sprintf("%s %s", command, environment)
	containerOptions, err := s.containerService.CreateOptions(project, commandVex, imageToUse)
	if err != nil {
		return err
	}

	runCommand, err := s.containerService.BuildCommand(containerOptions)
	if err != nil {
		return err
	}

	_, err = s.commandExecutor.Execute(ctx, runCommand)
	return err
}
