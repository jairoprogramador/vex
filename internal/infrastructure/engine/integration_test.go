package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jairoprogramador/vex/internal/application"
	"github.com/jairoprogramador/vex/internal/infrastructure/git"
)

// Prueba de integración con Docker real. Se activa con:
//
//	VEX_IT_IMAGE=<imagen con vexd> VEX_IT_PIPELINE=<ruta de un pipeline de ejemplo> go test ./internal/infrastructure/engine/
//
// El pipeline de ejemplo del motor está en vex-engine/internal/ejecucion/testdata/ejemplo.
func TestIntegration_AttemptAndLogsWithRealDocker(t *testing.T) {
	image, pipelineExample := os.Getenv("VEX_IT_IMAGE"), os.Getenv("VEX_IT_PIPELINE")
	if image == "" || pipelineExample == "" {
		t.Skip("VEX_IT_IMAGE y VEX_IT_PIPELINE no definidos: integración con Docker omitida")
	}

	base := t.TempDir()
	store, space, material := filepath.Join(base, "almacen"), filepath.Join(base, "espacio"), filepath.Join(base, "material")
	for _, dir := range []string{store, space, material} {
		require.NoError(t, os.MkdirAll(dir, 0o777))
		require.NoError(t, os.Chmod(dir, 0o777)) // el contenedor corre como uid 1001
	}

	// Los repos "remotos" se clonan con el mismo clonador que usará la CLI.
	projectOrigin, pipelineOrigin := filepath.Join(base, "proyecto-origen"), filepath.Join(base, "pipeline-origen")
	require.NoError(t, os.MkdirAll(projectOrigin, 0o755))
	require.NoError(t, os.MkdirAll(pipelineOrigin, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectOrigin, "README.md"), []byte("mi app\n"), 0o644))
	require.NoError(t, exec.Command("cp", "-R", pipelineExample+"/.", pipelineOrigin).Run())
	for _, repo := range []string{projectOrigin, pipelineOrigin} {
		for _, args := range [][]string{
			{"init", "-q", "--initial-branch=main"}, {"add", "-A"},
			{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "inicial"},
		} {
			out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
			require.NoError(t, err, string(out))
		}
	}
	cloner := git.NewSourceCloner(filepath.Join(base, "cache"))
	project, err := cloner.Ensure(context.Background(), projectOrigin, "main")
	require.NoError(t, err)
	pipeline, err := cloner.Ensure(context.Background(), pipelineOrigin, "main")
	require.NoError(t, err)

	spec := application.ContainerSpec{
		Image: image,
		Env: []application.EnvVar{
			{Name: "VEX_ALMACEN", Value: "/vex/almacen"},
			{Name: "VEX_ESPACIO", Value: "/vex/espacio"},
			{Name: "VEX_MATERIAL", Value: "/vex/material"},
		},
		Mounts: []application.Mount{
			{Source: project.Path, Target: "/proyecto", ReadOnly: true},
			{Source: pipeline.Path, Target: "/pipeline", ReadOnly: true},
			{Source: store, Target: "/vex/almacen"},
			{Source: space, Target: "/vex/espacio"},
			{Source: material, Target: "/vex/material"},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	engine := NewDockerEngine()

	var events []application.EngineEvent
	result, err := engine.Attempt(ctx, spec, application.AttemptRequest{
		Environment:    "sand",
		Requester:      "integration-test",
		ProjectSource:  "/proyecto",
		ProjectCommit:  project.Commit,
		PipelineSource: "/pipeline",
		PipelineCommit: pipeline.Commit,
		UntilStep:      "test",
		Project:        application.ProjectMetadata{ID: "p1", Name: "vex-demo"},
	}, func(e application.EngineEvent) { events = append(events, e) })

	require.NoError(t, err)
	assert.Equal(t, application.AttemptSucceeded, result.Status)
	require.NotEmpty(t, events)
	assert.Equal(t, application.AttemptStarted, events[0].Kind)

	outputs, err := engine.Logs(ctx, spec, application.LogsRequest{AttemptID: result.AttemptID})
	require.NoError(t, err)
	assert.NotEmpty(t, outputs)
}
