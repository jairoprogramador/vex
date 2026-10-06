package docker

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jairoprogramador/vex/internal/application"
)

func TestBuildArgs(t *testing.T) {
	tests := []struct {
		name  string
		build application.ImageBuild
		want  []string
	}{
		{
			name:  "sin args usa el Dockerfile indicado",
			build: application.ImageBuild{Tag: "demo:latest", Dockerfile: "docker/MyDockerfile"},
			want:  []string{"build", "-t", "demo:latest", "-f", "docker/MyDockerfile", "."},
		},
		{
			name:  "Dockerfile vacío equivale a Dockerfile",
			build: application.ImageBuild{Tag: "demo:latest"},
			want:  []string{"build", "-t", "demo:latest", "-f", "Dockerfile", "."},
		},
		{
			name: "los args conservan el orden y no se interpretan",
			build: application.ImageBuild{
				Tag: "demo:latest", Dockerfile: "Dockerfile",
				Args: []application.BuildArg{{Name: "B", Value: "$(id -u); rm -rf /"}, {Name: "A", Value: "con espacios"}},
			},
			want: []string{
				"build",
				"--build-arg", "B=$(id -u); rm -rf /",
				"--build-arg", "A=con espacios",
				"-t", "demo:latest", "-f", "Dockerfile", ".",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, buildArgs(tt.build))
		})
	}
}
