package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jairoprogramador/vex/internal/application"
)

func fakePlatform(goos string, uid, gid int, rootless bool, asked *int) platform {
	return platform{
		goos: goos,
		uid:  func() int { return uid },
		gid:  func() int { return gid },
		rootless: func() bool {
			*asked++
			return rootless
		},
	}
}

func TestHostRunUser(t *testing.T) {
	tests := []struct {
		name     string
		goos     string
		uid, gid int
		rootless bool
		want     []string
	}{
		{"linux con el usuario del anfitrión", "linux", 1000, 1000, false, []string{"--user", "1000:1000", "-e", "HOME=/home/vex"}},
		{"linux con gid distinto", "linux", 1000, 100, false, []string{"--user", "1000:100", "-e", "HOME=/home/vex"}},
		{"linux rootless", "linux", 1000, 1000, true, []string{"--user", "0:0", "-e", "HOME=/home/vex"}},
		{"linux como root", "linux", 0, 0, false, []string{"--user", "0:0", "-e", "HOME=/home/vex"}},
		{"macOS no cambia nada", "darwin", 501, 20, false, nil},
		{"windows no cambia nada", "windows", -1, -1, false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			asked := 0

			got := hostRunUser(fakePlatform(tt.goos, tt.uid, tt.gid, tt.rootless, &asked))

			assert.Equal(t, tt.want, got)
			if tt.goos != "linux" {
				assert.Zero(t, asked, "fuera de Linux no se pregunta a docker")
			}
		})
	}
}

func TestDockerRunArgs_ElUsuarioVaAntesDeLaImagen(t *testing.T) {
	runUser := []string{"--user", "1000:1000", "-e", "HOME=/home/vex"}

	args := dockerRunArgs("n", testSpec, runUser)

	assert.Equal(t, []string{
		"run", "--rm", "-i", "--name", "n",
		"--user", "1000:1000", "-e", "HOME=/home/vex",
		"-e", "VEX_ALMACEN=/vex/almacen",
		"--mount", "type=bind,source=/host/proyecto,target=/proyecto,readonly",
		"--mount", `type=bind,"source=/host/alma,cen",target=/vex/almacen`,
		"vex-runtime:latest",
	}, args)
}

func TestDockerRunArgs_HomeNoSeRepite(t *testing.T) {
	spec := application.ContainerSpec{
		Image: "img",
		Env:   []application.EnvVar{{Name: "HOME", Value: "/otro"}, {Name: "X", Value: "1"}},
	}

	args := dockerRunArgs("n", spec, []string{"--user", "1000:1000", "-e", "HOME=/home/vex"})

	assert.Equal(t, []string{
		"run", "--rm", "-i", "--name", "n", "--user", "1000:1000", "-e", "HOME=/home/vex", "-e", "X=1", "img",
	}, args, "manda el HOME del contrato de imagen")
}

func TestDockerRunArgs_SinUsuarioElComandoEsElDeSiempre(t *testing.T) {
	spec := application.ContainerSpec{Image: "img", Env: []application.EnvVar{{Name: "HOME", Value: "/otro"}}}

	assert.Equal(t, []string{"run", "--rm", "-i", "--name", "n", "-e", "HOME=/otro", "img"}, dockerRunArgs("n", spec, nil))
}

func TestDockerEngine_ElUsuarioSeDecideUnaSolaVez(t *testing.T) {
	asked := 0
	engine := NewDockerEngine()
	engine.platform = func() platform { return fakePlatform("linux", 1000, 1000, false, &asked) }

	first := engine.hostUser()
	second := engine.hostUser()

	assert.Equal(t, []string{"--user", "1000:1000", "-e", "HOME=/home/vex"}, first)
	assert.Equal(t, first, second)
	assert.Equal(t, 1, asked, "cada operación lanza un contenedor: no se pregunta a docker en cada una")
}
