package vos

import (
	"errors"
	"net/url"
	"path/filepath"
	"strings"
)

const (
	DefaultPipelineUrl = "https://github.com/jairoprogramador/mydeploy.git"
	DefaultPipelineRef = "main"
)

type Pipeline struct {
	url string
	ref string
}

func NewPipeline(repoURL, ref string) (Pipeline, error) {
	if repoURL == "" {
		return Pipeline{}, errors.New("pipeline url is required")
	}

	if ref == "" {
		ref = "main"
	}

	repoURLConverted := repoURL
	if strings.HasPrefix(repoURL, "git@") && !strings.HasPrefix(repoURL, "ssh://") {
		repoURLConverted = "ssh://" + strings.Replace(repoURL, ":", "/", 1)
	}

	parsedURL, err := url.Parse(repoURLConverted)
	if err != nil {
		return Pipeline{}, errors.New("la URL del repositorio del pipeline no es válida")
	}

	if parsedURL.Scheme == "" {
		return Pipeline{}, errors.New("la URL del repositorio debe tener un esquema (ej: https, ssh)")
	}

	return Pipeline{
		url: repoURL,
		ref: ref,
	}, nil
}

func (p Pipeline) URL() string {
	return p.url
}

func (p Pipeline) Ref() string {
	return p.ref
}

func (p Pipeline) DirName() string {
	base := filepath.Base(p.url)
	return strings.TrimSuffix(base, ".git")
}
