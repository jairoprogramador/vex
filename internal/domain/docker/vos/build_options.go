package vos

import "errors"

type BuildOptions struct {
	image          ImageName
	args           map[string]string
	dockerfilePath string // ruta al Dockerfile; "" equivale a "Dockerfile" en el directorio actual
}

func NewBuildOptions(
	image ImageName,
	args map[string]string,
	dockerfilePath string,
) (BuildOptions, error) {

	if image == (ImageName{}) {
		return BuildOptions{}, errors.New("image is required")
	}

	return BuildOptions{
		image:          image,
		args:           args,
		dockerfilePath: dockerfilePath,
	}, nil
}

func (b BuildOptions) Image() ImageName {
	return b.image
}

func (b BuildOptions) Args() map[string]string {
	return b.args
}

// DockerfilePath devuelve la ruta al Dockerfile configurada por el usuario.
// Un valor vacío indica el Dockerfile por defecto ("Dockerfile") en el directorio actual.
func (b BuildOptions) DockerfilePath() string {
	return b.dockerfilePath
}
