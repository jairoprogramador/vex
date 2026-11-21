package vos

import "errors"

type EnvVar struct {
	name  string
	value string
}

func NewEnvVar(name, value string) (EnvVar, error) {
	if name == "" {
		return EnvVar{}, errors.New("name is required")
	}
	if value == "" {
		return EnvVar{}, errors.New("value is required")
	}
	return EnvVar{name: name, value: value}, nil
}

func (e EnvVar) Name() string  { return e.name }
func (e EnvVar) Value() string { return e.value }
