package vos

import "github.com/jairoprogramador/vex-client/internal/domain/common/vos"

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
