package vos_test

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/jairoprogramador/vex/internal/domain/project/vos"
	"github.com/stretchr/testify/assert"
)

func TestNewProjectID(t *testing.T) {
	t.Run("should create a ProjectID with the given value", func(t *testing.T) {
		// Arrange
		idValue := "test-id-123"

		// Act
		projectID := vos.NewProjectID(idValue)

		// Assert
		assert.Equal(t, idValue, projectID.String(), "The returned ProjectID should have the correct value")
	})
}

func TestGenerateProjectID(t *testing.T) {
	t.Run("should generate a consistent ID for the same inputs", func(t *testing.T) {
		// Arrange
		name := "vex"
		organization := "vex"
		team := "itachi"

		// Act
		id1 := vos.GenerateProjectID(name, organization, team)
		id2 := vos.GenerateProjectID(name, organization, team)

		// Assert
		assert.True(t, id1.Equals(id2), "ProjectIDs generated with the same inputs should be equal")
		assert.Equal(t, id1.String(), id2.String(), "String representation of IDs should be equal")
	})

	t.Run("should generate different IDs for different inputs", func(t *testing.T) {
		// Arrange
		name1, org1, team1 := "project1", "org1", "team1"
		name2, org2, team2 := "project2", "org2", "team2"

		// Act
		id1 := vos.GenerateProjectID(name1, org1, team1)
		id2 := vos.GenerateProjectID(name2, org2, team2)

		// Assert
		assert.False(t, id1.Equals(id2), "ProjectIDs generated with different inputs should not be equal")
	})

	t.Run("should generate a valid SHA256 hash", func(t *testing.T) {
		// Arrange
		name := "my-project"
		organization := "my-org"
		team := "my-team"
		expectedData := fmt.Sprintf("%s-%s-%s", name, organization, team)
		expectedHash := sha256.Sum256([]byte(expectedData))
		expectedIDValue := fmt.Sprintf("%x", expectedHash)

		// Act
		generatedID := vos.GenerateProjectID(name, organization, team)

		// Assert
		assert.Equal(t, expectedIDValue, generatedID.String(), "The generated ID should be a valid SHA256 hash of the inputs")
		assert.Len(t, generatedID.String(), 64, "SHA256 hash should have a length of 64 characters in hex format")
	})
}

func TestProjectID_String(t *testing.T) {
	t.Run("should return the string value of the ProjectID", func(t *testing.T) {
		// Arrange
		idValue := "my-awesome-id"
		projectID := vos.NewProjectID(idValue)

		// Act
		stringValue := projectID.String()

		// Assert
		assert.Equal(t, idValue, stringValue, "String() should return the internal value")
	})
}

func TestProjectID_Equals(t *testing.T) {
	t.Run("should return true for equal ProjectIDs", func(t *testing.T) {
		// Arrange
		id1 := vos.NewProjectID("same-id")
		id2 := vos.NewProjectID("same-id")

		// Act & Assert
		assert.True(t, id1.Equals(id2), "Equals() should return true for identical IDs")
	})

	t.Run("should return false for different ProjectIDs", func(t *testing.T) {
		// Arrange
		id1 := vos.NewProjectID("id-1")
		id2 := vos.NewProjectID("id-2")

		// Act & Assert
		assert.False(t, id1.Equals(id2), "Equals() should return false for different IDs")
	})

	t.Run("should return true when comparing generated IDs with same source", func(t *testing.T) {
		// Arrange
		id1 := vos.GenerateProjectID("app", "org", "team")
		id2 := vos.GenerateProjectID("app", "org", "team")

		// Act & Assert
		assert.True(t, id1.Equals(id2), "Equals() should return true for identical generated IDs")
	})

	t.Run("should return false when comparing generated IDs with different source", func(t *testing.T) {
		// Arrange
		id1 := vos.GenerateProjectID("app1", "org1", "team1")
		id2 := vos.GenerateProjectID("app2", "org2", "team2")

		// Act & Assert
		assert.False(t, id1.Equals(id2), "Equals() should return false for different generated IDs")
	})
}
