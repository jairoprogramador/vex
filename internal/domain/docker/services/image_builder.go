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

// CreateOptions encapsula la lógica de negocio para determinar cómo se debe
// construir una imagen local a partir del Dockerfile del proyecto.
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

	// Image().Image() contiene la ruta del Dockerfile cuando tagExplicit=false.
	dockerfilePath := project.Runtime().Image().Image()
	return docVos.NewBuildOptions(imgName, imgArgs, dockerfilePath)
}

// BuildCommand devuelve el comando de build para la imagen.
// Soporta rutas de Dockerfile arbitrarias:
//   - "Dockerfile"              → docker build ... -f Dockerfile .
//   - "MyDockerfile"            → docker build ... -f MyDockerfile .
//   - "user/project/Dockerfile" → docker build ... -f user/project/Dockerfile user/project
func (s *imageBuilder) BuildCommand(opts docVos.BuildOptions) (string, error) {
	var commandBuilder strings.Builder
	commandBuilder.WriteString("docker build")

	for key, val := range opts.Args() {
		commandBuilder.WriteString(fmt.Sprintf(" --build-arg %s=%s", key, val))
	}

	commandBuilder.WriteString(fmt.Sprintf(" -t %s", opts.Image().FullName()))

	dockerfilePath := opts.DockerfilePath()
	if dockerfilePath == "" {
		dockerfilePath = "Dockerfile"
	}

	commandBuilder.WriteString(fmt.Sprintf(" -f %s %s", dockerfilePath, "."))

	return commandBuilder.String(), nil
}
