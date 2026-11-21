package vos

import "errors"

const (
	DefaultStateBackend = "local"
	DefaultStateURL     = ""
)

type State struct {
	backend string
	url     string
}

func NewState(backend, url string) (State, error) {
	if backend == "" {
		return State{}, errors.New("backend is required")
	}
	return State{backend: backend, url: url}, nil
}

func (s State) Backend() string { return s.backend }
func (s State) URL() string     { return s.url }
