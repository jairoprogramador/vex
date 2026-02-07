package vos_test

import (
	"testing"

	"github.com/jairoprogramador/vex/internal/domain/project/vos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTemplateRepository(t *testing.T) {
	t.Run("should create a template https repository successfully", func(t *testing.T) {
		// Arrange
		repoURL := "https://github.com/user/my-templates.git"
		ref := "v1.0.0"

		// Act
		repo, err := vos.NewTemplateRepository(repoURL, ref)

		// Assert
		require.NoError(t, err)
		assert.Equal(t, repoURL, repo.URL())
		assert.Equal(t, ref, repo.Ref())
	})

	t.Run("should create a template ssh repository successfully", func(t *testing.T) {
		// Arrange
		repoURL := "git@github.com:user/my-templates.git"
		ref := "v1.0.0"

		// Act
		repo, err := vos.NewTemplateRepository(repoURL, ref)

		// Assert
		require.NoError(t, err)
		assert.Equal(t, repoURL, repo.URL())
		assert.Equal(t, ref, repo.Ref())
	})

	t.Run("should default to 'main' when ref is empty", func(t *testing.T) {
		// Arrange
		repoURL := "https://github.com/user/my-templates.git"

		// Act
		repo, err := vos.NewTemplateRepository(repoURL, "")

		// Assert
		require.NoError(t, err)
		assert.Equal(t, "main", repo.Ref())
	})

	t.Run("should return an error when repo URL is empty", func(t *testing.T) {
		// Act
		_, err := vos.NewTemplateRepository("", "main")

		// Assert
		require.Error(t, err)
	})

	t.Run("should return an error for an invalid repo URL", func(t *testing.T) {
		// Act
		_, err := vos.NewTemplateRepository(":", "main")

		// Assert
		require.Error(t, err)
	})
}

func TestTemplateRepository_DirName(t *testing.T) {
	testCases := []struct {
		name        string
		repoURL     string
		expectedDir string
	}{
		{
			name:        "https url with .git suffix",
			repoURL:     "https://github.com/jairoprogramador/vex-templates.git",
			expectedDir: "vex-templates",
		},
		{
			name:        "https url without .git suffix",
			repoURL:     "https://github.com/jairoprogramador/vex-templates",
			expectedDir: "vex-templates",
		},
		{
			name:        "ssh url",
			repoURL:     "git@github.com:jairoprogramador/vex-templates.git",
			expectedDir: "vex-templates",
		},
		{
			name:        "url with nested path",
			repoURL:     "https://gitlab.com/my-org/my-team/my-templates.git",
			expectedDir: "my-templates",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			repo, err := vos.NewTemplateRepository(tc.repoURL, "main")
			require.NoError(t, err, "Test setup should not fail")

			// Act
			dirName := repo.DirName()

			// Assert
			assert.Equal(t, tc.expectedDir, dirName)
		})
	}
}
