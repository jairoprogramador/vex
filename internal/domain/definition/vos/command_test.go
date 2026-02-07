package vos_test

import (
	"testing"

	"github.com/jairoprogramador/vex/internal/domain/definition/vos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCommandDefinition(t *testing.T) {
	// Preparamos unos outputs válidos para reutilizar en los tests
	output1, _ := vos.NewOutputDefinition("out1", "desc1", "probe1")
	output2, _ := vos.NewOutputDefinition("out2", "desc2", "probe2")

	t.Run("should create a valid command with only required fields", func(t *testing.T) {
		cmd, err := vos.NewCommandDefinition("list files", "ls -la")
		require.NoError(t, err)
		assert.Equal(t, "list files", cmd.Name())
		assert.Equal(t, "ls -la", cmd.Cmd())
		assert.Empty(t, cmd.Description())
		assert.Empty(t, cmd.Workdir())
		assert.Empty(t, cmd.TemplateFiles())
		assert.Empty(t, cmd.Outputs())
	})

	t.Run("should create a valid command with all options", func(t *testing.T) {
		cmd, err := vos.NewCommandDefinition(
			"Terraform Apply",
			"terraform apply plan.out",
			vos.WithDescription("Applies the terraform plan"),
			vos.WithWorkdir("./terraform"),
			vos.WithTemplateFiles([]string{"plan.tfvars", "backend.tf"}),
			vos.WithOutputs([]vos.OutputDefinition{output1, output2}),
		)

		require.NoError(t, err)
		assert.Equal(t, "Terraform Apply", cmd.Name())
		assert.Equal(t, "terraform apply plan.out", cmd.Cmd())
		assert.Equal(t, "Applies the terraform plan", cmd.Description())
		assert.Equal(t, "./terraform", cmd.Workdir())
		assert.Equal(t, []string{"plan.tfvars", "backend.tf"}, cmd.TemplateFiles())
		assert.Equal(t, []vos.OutputDefinition{output1, output2}, cmd.Outputs())
	})

	t.Run("should return error if name is empty", func(t *testing.T) {
		_, err := vos.NewCommandDefinition("", "some command")
		require.Error(t, err)
	})

	t.Run("should return error if cmd is empty", func(t *testing.T) {
		_, err := vos.NewCommandDefinition("some name", "")
		require.Error(t, err)
	})

	t.Run("should return error for duplicate template files", func(t *testing.T) {
		_, err := vos.NewCommandDefinition(
			"test",
			"cmd",
			vos.WithTemplateFiles([]string{"file.txt", "another.txt", "file.txt"}),
		)
		require.Error(t, err)
	})

	t.Run("should return error for duplicate output names", func(t *testing.T) {
		duplicateOutput, _ := vos.NewOutputDefinition("out1", "desc_dup", "probe_dup")
		_, err := vos.NewCommandDefinition(
			"test",
			"cmd",
			vos.WithOutputs([]vos.OutputDefinition{output1, output2, duplicateOutput}),
		)
		require.Error(t, err)
	})
}

func TestCommandDefinition_Getters(t *testing.T) {
	t.Run("should return copies of slices to ensure immutability", func(t *testing.T) {
		// Arrange
		originalTemplates := []string{"a.txt"}
		output1, _ := vos.NewOutputDefinition("out1", "d", "p")
		originalOutputs := []vos.OutputDefinition{output1}

		cmd, err := vos.NewCommandDefinition(
			"test", "cmd",
			vos.WithTemplateFiles(originalTemplates),
			vos.WithOutputs(originalOutputs),
		)
		require.NoError(t, err)

		// Act: Modify the slices returned by getters
		templatesCopy := cmd.TemplateFiles()
		templatesCopy[0] = "b.txt"

		outputsCopy := cmd.Outputs()
		output2, _ := vos.NewOutputDefinition("out2", "d2", "p2")
		outputsCopy[0] = output2

		// Assert: The original slices inside the command object should not have changed
		assert.Equal(t, []string{"a.txt"}, cmd.TemplateFiles(), "TemplateFiles getter should return a copy")
		assert.Equal(t, []vos.OutputDefinition{output1}, cmd.Outputs(), "Outputs getter should return a copy")
	})
}
