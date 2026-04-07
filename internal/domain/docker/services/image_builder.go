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
type imageBuilder struct{}

// NewImageBuilder crea una nueva instancia del servicio.
func NewImageBuilder() docPor.ImageService {
	return &imageBuilder{}
}

// CreateImageOptions encapsula la lógica de negocio para determinar cómo se debe construir una imagen.
func (s *imageBuilder) CreateOptions(project *proAgg.Project) (docVos.BuildOptions, error) {
	localImageName := fmt.Sprintf("%s%s", project.Data().Name(), project.ID().String()[0:6])
	imgName, err := docVos.NewImageName(localImageName, project.Runtime().Image().Tag())
	if err != nil {
		return docVos.BuildOptions{}, err
	}

	imgArgs := make(map[string]string)

	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		imgArgs["DEV_GID"] = "$(id -g)"
		imgArgs["DEV_UID"] = "$(id -u)"
	}

	for _, arg := range project.Runtime().Args() {
		imgArgs[arg.Name()] = arg.Value()
	}

	return docVos.NewBuildOptions(imgName, imgArgs)
}

// BuildCommand devuelve el comando de build para la imagen.
func (s *imageBuilder) BuildCommand(opts docVos.BuildOptions) (string, error) {
	var commandBuilder strings.Builder
	commandBuilder.WriteString("docker build")

	for key, val := range opts.Args() {
		commandBuilder.WriteString(fmt.Sprintf(" --build-arg %s=%s", key, val))
	}

	commandBuilder.WriteString(fmt.Sprintf(" -t %s", opts.Image().FullName()))

	commandBuilder.WriteString(fmt.Sprintf(" %s", "."))

	//fmt.Println(commandBuilder.String())

	return commandBuilder.String(), nil
}
