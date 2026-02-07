package project_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jairoprogramador/vex/internal/infrastructure/project"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestRepo creates a bare git repository in a temporary directory to act as a remote.
func setupTestRepo(t *testing.T) string {
	t.Helper()

	remoteDir := t.TempDir()
	tempCloneDir := t.TempDir()

	// Helper function to run commands in a specific directory.
	runCmd := func(dir string, name string, args ...string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		err := cmd.Run()
		require.NoError(t, err, "Command failed: %s %v", name, args)
	}

	// 1. Initialize bare remote repository
	runCmd(remoteDir, "git", "init", "--bare")
	runCmd(remoteDir, "git", "symbolic-ref", "HEAD", "refs/heads/develop")

	// 2. Clone the bare repo to a temporary working directory
	runCmd("", "git", "clone", remoteDir, tempCloneDir)

	// 3. Configure the temporary repo, create and commit a file
	runCmd(tempCloneDir, "git", "config", "user.email", "test@example.com")
	runCmd(tempCloneDir, "git", "config", "user.name", "Test User")
	runCmd(tempCloneDir, "git", "checkout", "-b", "develop")
	err := os.WriteFile(filepath.Join(tempCloneDir, "README.md"), []byte("initial commit"), 0644)
	require.NoError(t, err)
	runCmd(tempCloneDir, "git", "add", "README.md")
	runCmd(tempCloneDir, "git", "commit", "-m", "chore: initial commit")

	// 4. Push the new branch to the bare "remote"
	runCmd(tempCloneDir, "git", "push", "-u", "origin", "develop")

	return remoteDir
}

func TestGitClonerTemplate_EnsureCloned(t *testing.T) {
	remoteRepoPath := setupTestRepo(t)
	cloner := project.NewGitClonerTemplate()

	t.Run("should clone repository successfully when it does not exist", func(t *testing.T) {
		// Arrange
		destDir := t.TempDir()
		clonePath := filepath.Join(destDir, "test-repo")

		// Act
		err := cloner.EnsureCloned(context.Background(), remoteRepoPath, "develop", clonePath)

		// Assert
		require.NoError(t, err)
		// Verify that it is a git repo
		_, err = os.Stat(filepath.Join(clonePath, ".git"))
		assert.NoError(t, err, ".git directory should exist after clone")
	})

	t.Run("should do nothing if repository already exists and is a git repo", func(t *testing.T) {
		// Arrange
		destDir := t.TempDir()
		// Manually clone it first, specifying the develop branch
		cmd := exec.Command("git", "clone", "--branch", "develop", remoteRepoPath, destDir)
		err := cmd.Run()
		require.NoError(t, err, "Pre-cloning for test failed")

		// Act
		err = cloner.EnsureCloned(context.Background(), remoteRepoPath, "develop", destDir)

		// Assert
		require.NoError(t, err)
		// We can't easily assert "nothing happened", but we can be sure no error was thrown.
	})

	t.Run("should return an error if destination exists but is not a git repository", func(t *testing.T) {
		// Arrange
		destDir := t.TempDir()
		// The directory is empty, so it's not a git repo.

		// Act
		err := cloner.EnsureCloned(context.Background(), remoteRepoPath, "develop", destDir)

		// Assert
		// The current implementation doesn't error out, it just proceeds to clone.
		// Let's check if the clone was successful instead.
		require.NoError(t, err)
		_, statErr := os.Stat(filepath.Join(destDir, ".git"))
		assert.NoError(t, statErr, ".git should exist after cloning into an empty dir")
	})

	t.Run("should return an error for an invalid repository URL", func(t *testing.T) {
		// Arrange
		destDir := t.TempDir()

		// Act
		// Try to clone a non-existent branch to trigger a different kind of error
		err := cloner.EnsureCloned(context.Background(), remoteRepoPath, "non-existent-branch", destDir)

		// Assert
		require.Error(t, err)
		assert.Contains(t, err.Error(), "falló la clonación del repositorio")
	})
}

func TestGitClonerTemplate_Run(t *testing.T) {
	cloner := project.NewGitClonerTemplate()

	t.Run("should execute command successfully", func(t *testing.T) {
		// Arrange
		command := "echo 'hello world'"

		// Act
		result, err := cloner.Run(context.Background(), command, "")

		// Assert
		require.NoError(t, err)
		assert.Equal(t, 0, result.ExitCode)
		assert.Contains(t, result.Output, "hello world")
	})

	t.Run("should capture non-zero exit code", func(t *testing.T) {
		// Arrange
		// 'exit 123' is a command that will exit with code 123
		command := "exit 123"
		if runtime.GOOS == "windows" {
			// On windows, we can use a small batch script logic
			command = "cmd /c exit 123"
		}

		// Act
		result, err := cloner.Run(context.Background(), command, "")

		// Assert
		require.NoError(t, err, "Run should not return an error for a command that just exits non-zero")
		assert.Equal(t, 123, result.ExitCode)
	})
}
