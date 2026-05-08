package dto

type PipelineDTO struct {
	URL string `yaml:"url"`
	Ref string `yaml:"ref,omitempty"`
}
