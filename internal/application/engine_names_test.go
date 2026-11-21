package application

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectMetadata_ValidaLoMismoQueElMotor(t *testing.T) {
	valid := ProjectMetadata{ID: "9d8d4777-5707-43b3-a636-d6f8433b7a34", Name: "mi-servicio_v2.1", Organization: "Vex", Team: "equipo"}
	require.NoError(t, valid.Validate())
	require.NoError(t, ProjectMetadata{}.Validate(), "un metadato vacío vale")

	tests := map[string]struct {
		change func(*ProjectMetadata)
		where  string
	}{
		"nombre con espacios":       {func(m *ProjectMetadata) { m.Name = "Pagos API" }, "project.name en vexconfig.yaml"},
		"nombre con acento":         {func(m *ProjectMetadata) { m.Name = "facturación" }, "project.name en vexconfig.yaml"},
		"nombre que inyecta":        {func(m *ProjectMetadata) { m.Name = "x;rm -rf /" }, "project.name en vexconfig.yaml"},
		"nombre que empieza por -":  {func(m *ProjectMetadata) { m.Name = "-rf" }, "project.name en vexconfig.yaml"},
		"nombre de 129":             {func(m *ProjectMetadata) { m.Name = strings.Repeat("a", 129) }, "project.name en vexconfig.yaml"},
		"organización con $":        {func(m *ProjectMetadata) { m.Organization = "$(id)" }, "project.organization en vexconfig.yaml"},
		"equipo con salto de línea": {func(m *ProjectMetadata) { m.Team = "a\nb" }, "project.team en vexconfig.yaml"},
		"id con barra":              {func(m *ProjectMetadata) { m.ID = "a/b" }, "project.id en vexconfig.yaml"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			metadata := valid
			tt.change(&metadata)

			err := metadata.Validate()

			var invalid *InvalidEngineNameError
			require.ErrorAs(t, err, &invalid)
			assert.Equal(t, tt.where, invalid.Where)
			assert.Contains(t, err.Error(), "→ Corrige "+tt.where)
			assert.NotContains(t, err.Error(), "rm -rf", "el mensaje no repite el valor")
		})
	}
	assert.NoError(t, ProjectMetadata{Name: strings.Repeat("a", 128)}.Validate())
}

func TestValidateEngineEnvironment(t *testing.T) {
	for _, name := range []string{"sand", "Prod", "qa_2", "a.b-c"} {
		assert.NoError(t, ValidateEngineEnvironment(name), name)
	}
	for _, name := range []string{"sand box", "-x", "sánd", "a;b", "$(id)", strings.Repeat("a", 129)} {
		err := ValidateEngineEnvironment(name)
		assert.ErrorContains(t, err, "El nombre del ambiente no es válido", name)
	}
}

func TestInvalidEngineField_SoloConoceLosMetadatos(t *testing.T) {
	invalid, ok := InvalidEngineField("Metadatos.ProjectTeam")
	require.True(t, ok)
	assert.Equal(t, "project.team en vexconfig.yaml", invalid.Where)

	_, ok = InvalidEngineField("Ambiente")
	assert.False(t, ok, "un ambiente rechazado lo explica quien sabe cuáles existen")
}

func TestLocalExecutor_NoLanzaNadaConUnNombreQueElMotorRechazaria(t *testing.T) {
	t.Run("ambiente", func(t *testing.T) {
		h := newHarness(t, newProject(t))

		err := h.executor.Run(context.Background(), "deploy", "sand; rm -rf /")

		assert.ErrorContains(t, err, "El nombre del ambiente no es válido")
		assert.Empty(t, h.sources.calls, "ni se clona")
		assert.Zero(t, h.engine.attempts)
	})
	t.Run("nombre del proyecto", func(t *testing.T) {
		h := newHarness(t, newProject(t, withName("Pagos API")))

		err := h.executor.Run(context.Background(), "deploy", "sand")

		var invalid *InvalidEngineNameError
		require.ErrorAs(t, err, &invalid)
		assert.Equal(t, "project.name en vexconfig.yaml", invalid.Where)
		assert.Empty(t, h.sources.calls)
		assert.Zero(t, h.engine.attempts)
	})
}

func TestRollback_NoLanzaNadaConUnMetadatoQueElMotorRechazaria(t *testing.T) {
	h := newRollbackHarness(t, withName("Pagos API"))

	err := h.service.Run(context.Background(), RollbackPlan{DeploymentID: idB, Environment: "prod"})

	var invalid *InvalidEngineNameError
	require.ErrorAs(t, err, &invalid)
	assert.Empty(t, h.sources.calls)
	assert.Zero(t, h.engine.attempts)
}
