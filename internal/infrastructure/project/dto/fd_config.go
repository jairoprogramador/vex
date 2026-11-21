package dto

type FDConfigDTO struct {
	Project  ProjectDTO  `yaml:"project"`
	Pipeline PipelineDTO `yaml:"pipeline"`
	Runtime  RuntimeDTO  `yaml:"runtime"`
	Cloud    *CloudDTO   `yaml:"cloud,omitempty"`
	Mode     string      `yaml:"mode,omitempty"`
}
