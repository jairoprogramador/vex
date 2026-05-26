package dto

type RuntimeDTO struct {
	Image string   `yaml:"image,omitempty"`
	Build BuildDTO `yaml:"build,omitempty"`
	Run   RunDTO   `yaml:"run,omitempty"`
}
