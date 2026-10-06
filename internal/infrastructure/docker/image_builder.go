// Package docker adapta la CLI de docker a los puertos de la aplicación.
package docker

import (
	"context"
	"fmt"
	"io"
	"os/exec"

	"github.com/jairoprogramador/vex/internal/application"
)

// ImageBuilder construye imágenes con `docker build`. Usa exec con argumentos,
// sin shell: ningún valor del vexconfig.yaml se interpreta.
type ImageBuilder struct {
	// output recibe lo que escribe docker build, para que el usuario vea el avance.
	output io.Writer
}

var _ application.ImageBuilder = (*ImageBuilder)(nil)

func NewImageBuilder(output io.Writer) *ImageBuilder {
	return &ImageBuilder{output: output}
}

func (b *ImageBuilder) Build(ctx context.Context, build application.ImageBuild) error {
	cmd := exec.CommandContext(ctx, "docker", buildArgs(build)...)
	cmd.Dir = build.ContextDir
	cmd.Stdout = b.output
	cmd.Stderr = b.output

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("docker build %s: %w", build.Tag, err)
	}
	return nil
}

// buildArgs arma los argumentos de docker build. El contexto es siempre el
// directorio de trabajo ("."); el Dockerfile se indica con -f.
func buildArgs(build application.ImageBuild) []string {
	args := []string{"build"}
	for _, arg := range build.Args {
		args = append(args, "--build-arg", arg.Name+"="+arg.Value)
	}
	dockerfile := build.Dockerfile
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	return append(args, "-t", build.Tag, "-f", dockerfile, ".")
}
