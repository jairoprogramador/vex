package application

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	docPor "github.com/jairoprogramador/vex/internal/domain/docker/ports"
	docVos "github.com/jairoprogramador/vex/internal/domain/docker/vos"
	proPor "github.com/jairoprogramador/vex/internal/domain/project/ports"
	proVos "github.com/jairoprogramador/vex/internal/domain/project/vos"
	"github.com/jairoprogramador/vex/internal/infrastructure/project/mapper"
)

const (
	MessageProjectNotInitialized = "project not initialized. Please run 'vex init' first"

	// requestInputEnvVar es la env var que `vexd run` lee para obtener el
	// RequestInput JSON. La codificamos en base64 para evitar que las
	// comillas/saltos de línea del JSON rompan el shell que invoca docker.
	requestInputEnvVar = "VEX_REQUEST_INPUT"
)

type LocalExecutorService struct {
	projectRepository proPor.ProjectRepository
	commandExecutor   docPor.CommandExecutor
	imageService      docPor.ImageService
	containerService  docPor.ContainerService
}

func NewLocalExecutorService(
	projectRepository proPor.ProjectRepository,
	commandExecutor docPor.CommandExecutor,
	imageService docPor.ImageService,
	containerService docPor.ContainerService,
) *LocalExecutorService {
	return &LocalExecutorService{
		projectRepository: projectRepository,
		commandExecutor:   commandExecutor,
		imageService:      imageService,
		containerService:  containerService,
	}
}

// Run ejecuta el step solicitado en el environment dado contra el contenedor
// runtime del proyecto. Construye el RequestInput JSON, lo serializa y codifica
// en base64, y lo inyecta como env var VEX_REQUEST_INPUT al `docker run`. El
// ENTRYPOINT de la imagen runtime debe ser `vexd run` (M3+, imagen :v2).
func (s *LocalExecutorService) Run(ctx context.Context, command, environment string) error {
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

	imageInfo := project.Runtime().Image()

	var imageToUse docVos.ImageName
	if !imageInfo.TagExplicit() {
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

	requestInput, err := mapper.ToRequestInput(project, command, environment)
	if err != nil {
		return fmt.Errorf("build request input: %w", err)
	}

	encoded, err := encodeRequestInput(requestInput)
	if err != nil {
		return fmt.Errorf("encode request input: %w", err)
	}

	// La env var con el RequestInput se inyecta en project.Runtime().Env(); el
	// containerService la traslada a -e VEX_REQUEST_INPUT=<base64> en docker run.
	envVar, err := proVos.NewEnvVar(requestInputEnvVar, encoded)
	if err != nil {
		return fmt.Errorf("build env var %s: %w", requestInputEnvVar, err)
	}
	project.SetRuntime(project.Runtime().WithExtraEnv(envVar))

	// Pasa --mode local explícitamente al ENTRYPOINT `vexd run`.
	// El string se append al comando docker run tras la imagen, por lo que el
	// proceso efectivo dentro del container es: vexd run --mode local
	containerOptions, err := s.containerService.CreateOptions(project, "--mode local", imageToUse)
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

// Compile-time contract check: ExecutorService satisfies Runner so the
// factory can return either local or remote implementations behind the
// same interface.
var _ Runner = (*LocalExecutorService)(nil)

// encodeRequestInput serializa el RequestInput a JSON y lo codifica en base64.
// El base64 es la forma portable: docker run --env recibe valores arbitrarios,
// pero la línea entera pasa por el shell del SO (Unix o PowerShell) y los
// saltos / comillas del JSON se escapan distinto. Base64 evita ese problema.
func encodeRequestInput(input mapper.RequestInputJSON) (string, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("marshal request input: %w", err)
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}
