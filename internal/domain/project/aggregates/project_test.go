package aggregates_test

import (
	"testing"

	"github.com/jairoprogramador/vex/internal/domain/project/aggregates"
	"github.com/jairoprogramador/vex/internal/domain/project/vos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setup creates a consistent set of VOs for testing
func setupProjectData(t *testing.T) (vos.ProjectData, vos.TemplateRepository) {
	data, err := vos.NewProjectData("my-app", "my-org", "my-team", "description", "1.0.0")
	require.NoError(t, err)

	repo, err := vos.NewTemplateRepository("https://github.com/templates/go-cli.git", "main")
	require.NoError(t, err)

	return data, repo
}

func TestNewProject(t *testing.T) {
	t.Run("should create a new project aggregate and return correct values", func(t *testing.T) {
		// Arrange
		data, repo := setupProjectData(t)
		id := vos.GenerateProjectID(data.Name(), data.Organization(), data.Team())
		projectPath := "/path/to/project"

		// Act
		project := aggregates.NewProject(id, data, repo, projectPath)

		// Assert
		require.NotNil(t, project)
		assert.True(t, id.Equals(project.ID()), "ID should match")
		assert.Equal(t, data, project.Data(), "Data should match")
		assert.Equal(t, repo, project.TemplateRepo(), "Template repository should match")
		assert.False(t, project.IsIDDirty(), "A new project should not be dirty")
	})
}

func TestProject_SyncID(t *testing.T) {
	t.Run("should return false when ID is already in sync", func(t *testing.T) {
		// Arrange
		data, repo := setupProjectData(t)
		correctID := vos.GenerateProjectID(data.Name(), data.Organization(), data.Team())
		project := aggregates.NewProject(correctID, data, repo, "")

		// Act
		synced := project.SyncID()

		// Assert
		assert.False(t, synced, "SyncID should return false if ID was correct")
		assert.True(t, correctID.Equals(project.ID()), "ID should not have changed")
		assert.False(t, project.IsIDDirty(), "Project should not be marked as dirty")
	})

	t.Run("should return true and update ID when out of sync", func(t *testing.T) {
		// Arrange
		data, repo := setupProjectData(t)
		initialID := vos.NewProjectID("stale-id") // An old or incorrect ID
		project := aggregates.NewProject(initialID, data, repo, "")

		// Act
		synced := project.SyncID()

		// Assert
		assert.True(t, synced, "SyncID should return true as the ID was updated")
		assert.False(t, initialID.Equals(project.ID()), "ID should have been updated")

		expectedID := vos.GenerateProjectID(data.Name(), data.Organization(), data.Team())
		assert.True(t, expectedID.Equals(project.ID()), "ID should be updated to the correct generated value")
		assert.True(t, project.IsIDDirty(), "Project should be marked as dirty after ID sync")
	})
}
