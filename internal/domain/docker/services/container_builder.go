package services

import (
	"fmt"
	"runtime"
	"strings"

	docPor "github.com/jairoprogramador/vex/internal/domain/docker/ports"
	docVos "github.com/jairoprogramador/vex/internal/domain/docker/vos"
	proAgg "github.com/jairoprogramador/vex/internal/domain/project/aggregates"
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
		normalizedVal := s.normalizeEnvValueForOS(val)
		commandBuilder.WriteString(fmt.Sprintf(" -e %s=%s", key, normalizedVal))
	}

	for key, val := range opts.Volumes() {
		commandBuilder.WriteString(fmt.Sprintf(" --mount type=bind,source=%s,target=%s", key, val))
	}

	commandBuilder.WriteString(fmt.Sprintf(" %s", opts.Image().FullName()))
	if cmdTail := strings.TrimSpace(opts.Command()); cmdTail != "" {
		commandBuilder.WriteString(" ")
		commandBuilder.WriteString(cmdTail)
	}

	//fmt.Println(commandBuilder.String())
	return commandBuilder.String(), nil
}

func (s *containerBuilder) normalizeEnvValueForOS(val string) string {
	if runtime.GOOS != "windows" {
		return val
	}
	if !strings.HasPrefix(val, "$") {
		return val
	}
	valWin := strings.TrimPrefix(val, "$")
	if valWin == "" {
		return val
	}
	return "%" + valWin + "%"
}
