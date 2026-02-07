package project_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jairoprogramador/vex/internal/domain/project/ports"
	"github.com/jairoprogramador/vex/internal/infrastructure/project"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestYAMLProjectRepository_Load(t *testing.T) {
	t.Run("should load project config successfully", func(t *testing.T) {
		// Arrange
		repo := project.NewYAMLProjectRepository()
		tempDir := t.TempDir()
		filePath := filepath.Join(tempDir, "fdconfig.yaml")

		yamlContent := `
project:
  id: "proj_123"
  name: "my-app"
  organization: "my-org"
  team: "my-team"
  description: "A cool app"
  version: "1.0.0"
template:
  url: "https://github.com/templates/go.git"
  ref: "main"
`
		err := os.WriteFile(filePath, []byte(yamlContent), 0644)
		require.NoError(t, err)

		// Act
		config, err := repo.Load(context.Background(), filePath)

		// Assert
		require.NoError(t, err)
		assert.Equal(t, "proj_123", config.ID)
		assert.Equal(t, "my-app", config.Name)
		assert.Equal(t, "my-org", config.Organization)
		assert.Equal(t, "my-team", config.Team)
		assert.Equal(t, "A cool app", config.Description)
		assert.Equal(t, "1.0.0", config.Version)
		assert.Equal(t, "https://github.com/templates/go.git", config.TemplateURL)
		assert.Equal(t, "main", config.TemplateRef)
	})

	t.Run("should return error if file does not exist", func(t *testing.T) {
		// Arrange
		repo := project.NewYAMLProjectRepository()
		nonExistentPath := "/path/that/does/not/exist/fd.yaml"

		// Act
		_, err := repo.Load(context.Background(), nonExistentPath)

		// Assert
		require.Error(t, err)
	})

	t.Run("should return error for malformed yaml", func(t *testing.T) {
		// Arrange
		repo := project.NewYAMLProjectRepository()
		tempDir := t.TempDir()
		filePath := filepath.Join(tempDir, "fdmalformed.yaml")

		malformedYaml := "project: { name: 'my-app'" // Invalid YAML
		err := os.WriteFile(filePath, []byte(malformedYaml), 0644)
		require.NoError(t, err)

		// Act
		_, err = repo.Load(context.Background(), filePath)

		// Assert
		require.Error(t, err)
	})
}

func TestYAMLProjectRepository_Save(t *testing.T) {
	t.Run("should save project config successfully", func(t *testing.T) {
		// Arrange
		repo := project.NewYAMLProjectRepository()
		tempDir := t.TempDir()
		filePath := filepath.Join(tempDir, "fdsave.yaml")

		config := &ports.ProjectConfigDTO{
			ID:           "proj_456",
			Name:         "saved-app",
			Organization: "saved-org",
			Team:         "saved-team",
			Description:  "Saved description",
			Version:      "2.0.0",
			TemplateURL:  "https://github.com/saved/template.git",
			TemplateRef:  "develop",
		}

		// Act
		err := repo.Save(context.Background(), filePath, config)
		require.NoError(t, err)

		// Assert
		fileData, err := os.ReadFile(filePath)
		require.NoError(t, err)

		// A simple check to ensure key fields are present.
		// For a full assertion, you might unmarshal back and compare.
		assert.Contains(t, string(fileData), "id: proj_456")
		assert.Contains(t, string(fileData), "name: saved-app")
		assert.Contains(t, string(fileData), "url: https://github.com/saved/template.git")
		assert.Contains(t, string(fileData), "ref: develop")
	})
}

func TestYAMLProjectRepository_SaveAndLoad(t *testing.T) {
	t.Run("should load the exact same data that was saved", func(t *testing.T) {
		// Arrange
		repo := project.NewYAMLProjectRepository()
		tempDir := t.TempDir()
		filePath := filepath.Join(tempDir, "fd.yaml")

		originalConfig := &ports.ProjectConfigDTO{
			ID:           "cycle-id-789",
			Name:         "cycle-app",
			Organization: "cycle-org",
			Team:         "cycle-team",
			Description:  "Testing the full save/load cycle",
			Version:      "3.0.0-beta",
			TemplateURL:  "https://github.com/cycle/template.git",
			TemplateRef:  "feature-branch",
		}

		// Act: Save the config
		err := repo.Save(context.Background(), filePath, originalConfig)
		require.NoError(t, err, "Save operation should succeed")

		// Act: Load the config back
		loadedConfig, err := repo.Load(context.Background(), filePath)
		require.NoError(t, err, "Load operation should succeed")

		// Assert
		assert.Equal(t, originalConfig, loadedConfig, "Loaded config should be identical to the saved one")
	})
}
