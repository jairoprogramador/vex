package application

import "context"

// ImageBuilder construye una imagen local a partir de un Dockerfile del proyecto.
type ImageBuilder interface {
	Build(ctx context.Context, build ImageBuild) error
}

type ImageBuild struct {
	Tag string
	// Dockerfile es relativo a ContextDir, que también es el directorio de trabajo del build.
	Dockerfile string
	ContextDir string
	Args       []BuildArg
}

type BuildArg struct {
	Name  string
	Value string
}
