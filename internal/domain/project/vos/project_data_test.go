package vos_test

import (
	"testing"

	"github.com/jairoprogramador/vex/internal/domain/project/vos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProjectData(t *testing.T) {
	t.Run("should create project data successfully with all fields", func(t *testing.T) {
		// Arrange
		name := "MyProject"
		org := "MyOrg"
		team := "MyTeam"
		desc := "A sample project description"
		version := "1.0.0"

		// Act
		data, err := vos.NewProjectData(name, org, team, desc, version)

		// Assert
		require.NoError(t, err)
		assert.Equal(t, name, data.Name())
		assert.Equal(t, org, data.Organization())
		assert.Equal(t, team, data.Team())
		assert.Equal(t, desc, data.Description())
		assert.Equal(t, version, data.Version())
	})

	t.Run("should create project data successfully with only required fields", func(t *testing.T) {
		// Arrange
		name := "MyProject"
		org := "MyOrg"
		team := "MyTeam"

		// Act
		data, err := vos.NewProjectData(name, org, team, "", "")

		// Assert
		require.NoError(t, err)
		assert.Equal(t, name, data.Name())
		assert.Equal(t, org, data.Organization())
		assert.Equal(t, team, data.Team())
		assert.Equal(t, "", data.Description())
		assert.Equal(t, "", data.Version())
	})

	validationTestCases := []struct {
		name        string
		org         string
		team        string
		expectedErr string
	}{
		{"", "MyOrg", "MyTeam", "name is required"},
		{"MyProject", "", "MyTeam", "organization is required"},
		{"MyProject", "MyOrg", "", "team is required"},
	}

	for _, tc := range validationTestCases {
		t.Run("should return an error when "+tc.expectedErr, func(t *testing.T) {
			// Act
			_, err := vos.NewProjectData(tc.name, tc.org, tc.team, "desc", "1.0.0")

			// Assert
			require.Error(t, err)
			assert.Equal(t, tc.expectedErr, err.Error())
		})
	}
}

func TestProjectData_Getters(t *testing.T) {
	t.Run("should return correct values via getters", func(t *testing.T) {
		// Arrange
		name := "GetterTestProject"
		org := "GetterOrg"
		team := "GetterTeam"
		desc := "Description for getter test"
		version := "v2.alpha"

		data, err := vos.NewProjectData(name, org, team, desc, version)
		require.NoError(t, err, "Test setup should not fail")

		// Act & Assert
		assert.Equal(t, name, data.Name(), "Name() getter should return the correct name")
		assert.Equal(t, org, data.Organization(), "Organization() getter should return the correct organization")
		assert.Equal(t, team, data.Team(), "Team() getter should return the correct team")
		assert.Equal(t, desc, data.Description(), "Description() getter should return the correct description")
		assert.Equal(t, version, data.Version(), "Version() getter should return the correct version")
	})
}
