package application

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func newTestWorkspace(t *testing.T) (*Workspace, LocalExecutorConfig) {
	t.Helper()
	config := LocalExecutorConfig{
		WorkDir: "/work", StoreDir: "/host/almacen", SpaceDir: "/host/espacio", MaterialDir: "/host/material",
		Requester: "jailux",
	}
	return NewWorkspace(&stubProjectRepo{}, &fakeSources{}, &fakeImages{}, &fakePresenter{}, config), config
}

func TestWorkspace_UnaConsultaAlHistorialNoMontaClones(t *testing.T) {
	ws, config := newTestWorkspace(t)

	spec := ws.HistorySpec("img:1")

	assert.Equal(t, ContainerSpec{
		Image:  "img:1",
		Mounts: []Mount{{Source: config.StoreDir, Target: "/vex/almacen"}},
		Env:    []EnvVar{{Name: "VEX_ALMACEN", Value: "/vex/almacen"}},
	}, spec, "solo el historial: sin proyecto, sin pipeline, sin espacio de trabajo")
}

func TestWorkspace_LeerElPipelineMontaSoloElPipelineYElHistorial(t *testing.T) {
	ws, config := newTestWorkspace(t)

	spec := ws.PipelineSpec("img:1", Source{Path: "/cache/pipe", Commit: "abc"})

	assert.Equal(t, []Mount{
		{Source: "/cache/pipe", Target: "/pipeline", ReadOnly: true},
		{Source: config.StoreDir, Target: "/vex/almacen"},
	}, spec.Mounts)
	assert.Equal(t, []EnvVar{{Name: "VEX_ALMACEN", Value: "/vex/almacen"}}, spec.Env,
		"no necesita VEX_ESPACIO: no ejecuta comandos")
}

func TestWorkspace_EjecutarMontaTodoYFijaLasTresVariables(t *testing.T) {
	ws, _ := newTestWorkspace(t)

	spec := ws.ExecutionSpec("img:1", newProject(t), Source{Path: "/cache/app"}, Source{Path: "/cache/pipe"})

	targets := make([]string, len(spec.Mounts))
	for i, m := range spec.Mounts {
		targets[i] = m.Target
	}
	assert.Equal(t, []string{"/proyecto", "/pipeline", "/vex/almacen", "/vex/espacio", "/vex/material"}, targets)
	assert.Equal(t, []EnvVar{
		{Name: "VEX_ALMACEN", Value: "/vex/almacen"},
		{Name: "VEX_ESPACIO", Value: "/vex/espacio"},
		{Name: "VEX_MATERIAL", Value: "/vex/material"},
	}, spec.Env)
}

func TestWorkspace_ElPipelineSeVeConLaRutaDelContenedor(t *testing.T) {
	ws, _ := newTestWorkspace(t)

	assert.Equal(t, PipelineRef{Source: "/pipeline", Commit: "abc"}, ws.PipelineRef(Source{Path: "/cache/pipe", Commit: "abc"}))
}
