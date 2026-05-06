package vos

import "errors"

type RunOptions struct {
	image        ImageName
	volumes      map[string]string
	envVars      map[string]string
	command      string
	removeOnExit bool
}

func NewRunOptions(
	image ImageName,
	volumes map[string]string,
	envVars map[string]string,
	command string,
	removeOnExit bool) (RunOptions, error) {

	if image == (ImageName{}) {
		return RunOptions{}, errors.New("image is required")
	}
	// command es opcional: en modo one-shot (M3+) el ENTRYPOINT del contenedor
	// es `vexd run` y los datos van por env var, no por argumentos.

	return RunOptions{
		image:        image,
		volumes:      volumes,
		envVars:      envVars,
		command:      command,
		removeOnExit: removeOnExit,
	}, nil
}

func (r RunOptions) Image() ImageName {
	return r.image
}

func (r RunOptions) Volumes() map[string]string {
	return r.volumes
}

func (r RunOptions) EnvVars() map[string]string {
	return r.envVars
}

func (r RunOptions) Command() string {
	return r.command
}

func (r RunOptions) RemoveOnExit() bool {
	return r.removeOnExit
}
