package vos

import (
	"errors"
	"net/url"
	"strings"
	"path/filepath"
)

const (
	DefaultTemplateUrl = "https://github.com/jairoprogramador/mydeploy.git"
	DefaultTemplateRef = "main"
)

type Template struct {
	url string
	ref string
}

func NewTemplate(repoURL, ref string) (Template, error) {
	if repoURL == "" {
		return Template{}, errors.New("repoURL is required")
	}

	if ref == "" {
		return Template{}, errors.New("ref is required")
	}

	repoURLConverted := repoURL
	if strings.HasPrefix(repoURL, "git@") && !strings.HasPrefix(repoURL, "ssh://") {
		repoURLConverted = "ssh://" + strings.Replace(repoURL, ":", "/", 1)
	}

	parsedURL, err := url.Parse(repoURLConverted)
	if err != nil {
		return Template{}, errors.New("la URL del repositorio de plantillas no es válida")
	}

	if parsedURL.Scheme == "" {
		return Template{}, errors.New("la URL del repositorio debe tener un esquema (ej: https, ssh)")
	}

	return Template{
		url: repoURL,
		ref: ref,
	}, nil
}

func (t Template) URL() string {
	return t.url
}

func (t Template) Ref() string {
	return t.ref
}

func (t Template) DirName() string {
	base := filepath.Base(t.url)
	return strings.TrimSuffix(base, ".git")
}

