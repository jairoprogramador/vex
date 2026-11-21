package vos

import "errors"

type Volume struct {
	host      string
	container string
}

func NewVolume(host, container string) (Volume, error) {
	if host == "" {
		return Volume{}, errors.New("host is required")
	}
	if container == "" {
		return Volume{}, errors.New("container is required")
	}
	return Volume{host: host, container: container}, nil
}

func (v Volume) Host() string      { return v.host }
func (v Volume) Container() string { return v.container }
