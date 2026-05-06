package vos

import "github.com/jairoprogramador/vex/internal/domain/common/vos"

type Runtime struct {
	image   vos.Image
	volumes []Volume
	env     []EnvVar
	args    []Argument
}

type RuntimeOption func(*Runtime)

func WithImage(image vos.Image) RuntimeOption {
	return func(r *Runtime) { r.image = image }
}

func WithVolumes(volumes []Volume) RuntimeOption {
	return func(r *Runtime) { r.volumes = volumes }
}

func WithEnv(env []EnvVar) RuntimeOption {
	return func(r *Runtime) { r.env = env }
}

func WithArgs(args []Argument) RuntimeOption {
	return func(r *Runtime) { r.args = args }
}

func NewRuntime(opts ...RuntimeOption) Runtime {
	r := Runtime{}
	for _, opt := range opts {
		opt(&r)
	}
	return r
}

func (r Runtime) Image() vos.Image  { return r.image }
func (r Runtime) Volumes() []Volume { return r.volumes }
func (r Runtime) Env() []EnvVar     { return r.env }
func (r Runtime) Args() []Argument  { return r.args }

// WithExtraEnv retorna un nuevo Runtime con las env vars adicionales anexadas
// al final del slice existente. La VO permanece inmutable: el llamador debe
// reasignarla (ej. project.SetRuntime(...)).
func (r Runtime) WithExtraEnv(extra ...EnvVar) Runtime {
	if len(extra) == 0 {
		return r
	}
	merged := make([]EnvVar, 0, len(r.env)+len(extra))
	merged = append(merged, r.env...)
	merged = append(merged, extra...)
	return Runtime{
		image:   r.image,
		volumes: r.volumes,
		env:     merged,
		args:    r.args,
	}
}
