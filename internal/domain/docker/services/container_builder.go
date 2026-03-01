package services

import (
	"fmt"
	"strings"

	docPor "github.com/jairoprogramador/vex-client/internal/domain/docker/ports"
	docVos "github.com/jairoprogramador/vex-client/internal/domain/docker/vos"
	proAgg "github.com/jairoprogramador/vex-client/internal/domain/project/aggregates"
)

// imageBuilder es la implementación del servicio de dominio.
type containerBuilder struct{}

// NewImageBuilder crea una nueva instancia del servicio.
func NewContainerBuilder() docPor.ContainerService {
	return &containerBuilder{}
}

// CreateImageOptions encapsula la lógica de negocio para determinar cómo se debe construir una imagen.
func (s *containerBuilder) CreateOptions(project *proAgg.Project, commandVex string, image docVos.ImageName) (docVos.RunOptions, error) {
	volumes := make(map[string]string)

	for _, volume := range project.Runtime().Volumes() {
		volumes[volume.Host()] = volume.Container()
	}

	envVars := make(map[string]string)
	for _, envVar := range project.Runtime().Env() {
		envVars[envVar.Name()] = envVar.Value()
	}

	return docVos.NewRunOptions(
		image, volumes, envVars,
		commandVex, true)
}

// BuildCommand devuelve el comando de build para la imagen.
func (s *containerBuilder) BuildCommand(opts docVos.RunOptions) (string, error) {
	var commandBuilder strings.Builder
	commandBuilder.WriteString("docker run")

	if opts.RemoveOnExit() {
		commandBuilder.WriteString(" --rm")
	}

	for key, val := range opts.EnvVars() {
		commandBuilder.WriteString(fmt.Sprintf(" -e %s=%s", key, val))
	}

	for key, val := range opts.Volumes() {
		commandBuilder.WriteString(fmt.Sprintf(" --mount type=bind,source=%s,target=%s", key, val))
	}

	commandBuilder.WriteString(fmt.Sprintf(" %s", opts.Image().FullName()))
	commandBuilder.WriteString(fmt.Sprintf(" %s", strings.TrimSpace(opts.Command())))

	return commandBuilder.String(), nil
}
