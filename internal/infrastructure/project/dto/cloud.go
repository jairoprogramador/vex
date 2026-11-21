package dto

type CloudDTO struct {
	Provider    string `yaml:"provider,omitempty"`
	Credentials string `yaml:"credentials"`
}
