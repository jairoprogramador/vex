package aggregates_test

import (
	"testing"

	"github.com/jairoprogramador/vex/internal/domain/definition/aggregates"
	"github.com/jairoprogramador/vex/internal/domain/definition/entities"
	"github.com/jairoprogramador/vex/internal/domain/definition/vos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewExecutionPlanDefinition(t *testing.T) {
	// --- Arrange: Crear datos de prueba válidos ---

	// Entorno
	env, err := vos.NewEnvironment("stag", "Staging")
	require.NoError(t, err)

	// Step
	stepName, err := vos.NewStepNameDefinition("01-test")
	require.NoError(t, err)
	cmd, err := vos.NewCommandDefinition("test", "go test ./...")
	require.NoError(t, err)
	step, err := entities.NewStepDefinition(stepName, []vos.CommandDefinition{cmd}, nil)
	require.NoError(t, err)

	t.Run("should create a valid execution plan definition", func(t *testing.T) {
		// Act
		plan, err := aggregates.NewExecutionPlanDefinition(env, []*entities.StepDefinition{step})

		// Assert
		require.NoError(t, err)
		require.NotNil(t, plan)
		assert.Equal(t, env, plan.Environment())
		assert.Len(t, plan.Steps(), 1)
		assert.Equal(t, step, plan.Steps()[0])
	})

	t.Run("should return error if steps slice is empty", func(t *testing.T) {
		// Act
		_, err := aggregates.NewExecutionPlanDefinition(env, []*entities.StepDefinition{})

		// Assert
		require.Error(t, err)
	})

	t.Run("should return error if steps slice is nil", func(t *testing.T) {
		// Act
		_, err := aggregates.NewExecutionPlanDefinition(env, nil)

		// Assert
		require.Error(t, err)
	})
}
