package dto

type RunDTO struct {
	Volumes []VolumeDTO `yaml:"volumes,omitempty"`
	Env     []EnvVarDTO `yaml:"envs,omitempty"`
}
