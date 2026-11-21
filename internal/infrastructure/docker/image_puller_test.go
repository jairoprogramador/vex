package docker

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptedPuller sustituye a docker por un script de shell por subcomando ("pull", "image") y anota los argv.
func scriptedPuller(calls *[][]string, scripts map[string]string) *ImagePuller {
	return &ImagePuller{newCommand: func(ctx context.Context, args ...string) *exec.Cmd {
		*calls = append(*calls, args)
		return exec.CommandContext(ctx, "sh", "-c", scripts[args[0]])
	}}
}

func TestImagePuller_BajaLaImagenSinShell(t *testing.T) {
	var calls [][]string
	puller := scriptedPuller(&calls, map[string]string{"pull": "exit 0"})

	require.NoError(t, puller.Pull(context.Background(), "registro/runtime:v1; rm -rf /"))

	assert.Equal(t, [][]string{{"pull", "--quiet", "registro/runtime:v1; rm -rf /"}}, calls, "la imagen es un argumento, no se interpreta")
}

func TestImagePuller_SiNoSePuedeBajarValeLaImagenLocal(t *testing.T) {
	var calls [][]string
	puller := scriptedPuller(&calls, map[string]string{"pull": "echo 'pull access denied' >&2; exit 1", "image": "exit 0"})

	require.NoError(t, puller.Pull(context.Background(), "starter:test"))

	assert.Equal(t, []string{"image", "inspect", "--format", "{{.Id}}", "starter:test"}, calls[1])
}

func TestImagePuller_SinImagenLocalElErrorDiceLoQueDijoDocker(t *testing.T) {
	var calls [][]string
	puller := scriptedPuller(&calls, map[string]string{"pull": "echo 'manifest unknown' >&2; exit 1", "image": "exit 1"})

	err := puller.Pull(context.Background(), "registro/runtime:v9")

	assert.ErrorContains(t, err, "docker pull registro/runtime:v9")
	assert.ErrorContains(t, err, "manifest unknown")
}

func TestImagePuller_CancelarDetieneLaDescarga(t *testing.T) {
	var calls [][]string
	puller := scriptedPuller(&calls, map[string]string{"pull": "exec sleep 30"})
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)

	start := time.Now()
	err := puller.Pull(ctx, "registro/runtime:v1")

	assert.ErrorIs(t, err, context.Canceled)
	assert.Less(t, time.Since(start), 10*time.Second, "no espera a que docker termine")
	assert.Len(t, calls, 1, "tras cancelar no pregunta por la imagen local")
}

func TestImagePuller_ElErrorNoCreceSinLimite(t *testing.T) {
	var calls [][]string
	puller := scriptedPuller(&calls, map[string]string{"pull": "head -c 100000 /dev/zero | tr '\\0' 'x' >&2; exit 1", "image": "exit 1"})

	err := puller.Pull(context.Background(), "img:v1")

	require.Error(t, err)
	assert.Less(t, len(err.Error()), 5000)
	assert.True(t, strings.HasPrefix(err.Error(), "docker pull img:v1"))
}
