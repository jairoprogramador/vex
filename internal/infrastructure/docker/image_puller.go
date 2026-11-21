package docker

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/jairoprogramador/vex/internal/application"
)

// maxPullStderr es cuánto de lo que dice docker se conserva para explicar un fallo.
const maxPullStderr = 4 << 10

// ImagePuller baja imágenes con `docker pull`. Usa exec con argumentos, sin shell.
type ImagePuller struct {
	newCommand func(ctx context.Context, args ...string) *exec.Cmd
}

var _ application.ImagePuller = (*ImagePuller)(nil)

func NewImagePuller() *ImagePuller {
	return &ImagePuller{newCommand: func(ctx context.Context, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "docker", args...)
	}}
}

// Pull deja image disponible para `docker run`. Si no se puede bajar pero ya existe en este equipo (una imagen
// construida a mano, o sin red), vale la local: bajar la imagen es adelantar trabajo, no un requisito.
func (p *ImagePuller) Pull(ctx context.Context, image string) error {
	var stderr bytes.Buffer
	cmd := p.newCommand(ctx, "pull", "--quiet", image)
	cmd.Stderr = &stderr
	err := cmd.Run()
	switch {
	case err == nil:
		return nil
	case ctx.Err() != nil:
		return ctx.Err()
	case p.newCommand(ctx, "image", "inspect", "--format", "{{.Id}}", image).Run() == nil:
		return nil
	}
	detail := strings.TrimSpace(stderr.String())
	return fmt.Errorf("docker pull %s: %w: %s", image, err, detail[:min(len(detail), maxPullStderr)])
}
