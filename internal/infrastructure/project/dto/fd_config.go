package dto

type FDConfigDTO struct {
	Project  ProjectDTO  `yaml:"project"`
	Pipeline PipelineDTO `yaml:"pipeline"`
	Runtime  RuntimeDTO  `yaml:"runtime"`
	Mode     string      `yaml:"mode,omitempty"` // "remote" | "local" | omitido (default remoto)
}
