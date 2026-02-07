package entities_test

import (
	"testing"

	"github.com/jairoprogramador/vex/internal/domain/definition/entities"
	"github.com/jairoprogramador/vex/internal/domain/definition/vos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewStepDefinition(t *testing.T) {
	// --- Arrange: Datos de prueba válidos y reutilizables ---
	stepName, _ := vos.NewStepNameDefinition("01-test")
	cmd1, _ := vos.NewCommandDefinition("test", "go test ./...", vos.WithWorkdir("./"))
	cmd2, _ := vos.NewCommandDefinition("lint", "golangci-lint run", vos.WithWorkdir("./"))
	var1, _ := vos.NewVariableDefinition("VAR1", "value1")
	var2, _ := vos.NewVariableDefinition("VAR2", "value2")

	t.Run("should create a valid step definition with commands and variables", func(t *testing.T) {
		// Act
		step, err := entities.NewStepDefinition(
			stepName, []vos.CommandDefinition{cmd1, cmd2}, []vos.VariableDefinition{var1, var2})

		// Assert
		require.NoError(t, err)
		assert.NotNil(t, step)
		assert.Equal(t, stepName, step.NameDef())
		assert.Len(t, step.CommandsDef(), 2)
		assert.Len(t, step.VariablesDef(), 2)
	})

	t.Run("should create a valid step definition without variables", func(t *testing.T) {
		// Act
		step, err := entities.NewStepDefinition(
			stepName, []vos.CommandDefinition{cmd1}, []vos.VariableDefinition{})

		// Assert
		require.NoError(t, err)
		assert.NotNil(t, step)
		assert.Len(t, step.CommandsDef(), 1)
		assert.Empty(t, step.VariablesDef())
	})

	t.Run("should return error if commands slice is empty", func(t *testing.T) {
		// Act
		_, err := entities.NewStepDefinition(stepName, []vos.CommandDefinition{}, []vos.VariableDefinition{})

		// Assert
		require.Error(t, err)
	})

	t.Run("should return error for duplicate commands", func(t *testing.T) {
		// Arrange: cmd1 duplicado
		duplicateCommands := []vos.CommandDefinition{cmd1, cmd2, cmd1}

		// Act
		_, err := entities.NewStepDefinition(stepName, duplicateCommands, []vos.VariableDefinition{})

		// Assert
		require.Error(t, err)
	})

	t.Run("should return error for duplicate variables", func(t *testing.T) {
		// Arrange: var1 duplicada
		duplicateVars := []vos.VariableDefinition{var1, var2, var1}

		// Act
		_, err := entities.NewStepDefinition(stepName, []vos.CommandDefinition{cmd1}, duplicateVars)

		// Assert
		require.Error(t, err)
	})

	t.Run("should consider commands with different workdir as unique", func(t *testing.T) {
		// Arrange
		cmd1, _ := vos.NewCommandDefinition("build", "go build", vos.WithWorkdir("./app1"))
		cmd2, _ := vos.NewCommandDefinition("build", "go build", vos.WithWorkdir("./app2")) // Mismos name y cmd, diferente workdir

		// Act
		step, err := entities.NewStepDefinition(stepName, []vos.CommandDefinition{cmd1, cmd2}, []vos.VariableDefinition{})

		// Assert
		require.NoError(t, err)
		assert.NotNil(t, step)
		assert.Len(t, step.CommandsDef(), 2)
	})
}
