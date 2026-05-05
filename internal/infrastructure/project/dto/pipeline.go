package dto

type PipelineDTO struct {
	URL string `yaml:"url,omitempty"`
	Ref string `yaml:"ref,omitempty"`
}
