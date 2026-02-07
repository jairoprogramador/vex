package project

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/jairoprogramador/vex/internal/domain/project/ports"
)

type GitClonerTemplate struct {
}

func NewGitClonerTemplate() ports.ClonerTemplate {
	return &GitClonerTemplate{}
}

func isGitRepository(path string) (bool, error) {
	_, err := os.Stat(filepath.Join(path, ".git"))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (c *GitClonerTemplate) EnsureCloned(ctx context.Context, repoURL, ref, localPath string) error {
	if _, err := os.Stat(localPath); err == nil {
		isGit, err := isGitRepository(localPath)
		if err != nil {
			return fmt.Errorf("no se pudo verificar si la ruta es un repositorio git: %w", err)
		}

		if isGit {
			return nil
		}
	}

	command := fmt.Sprintf("git clone --branch %s %s %s", ref, repoURL, localPath)

	res, err := c.Run(ctx, command, "")
	if err != nil {
		return fmt.Errorf("no se pudo iniciar el comando de clonación: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("falló la clonación del repositorio '%s' (código %d): %s", repoURL, res.ExitCode, res.Output)
	}

	return nil
}

func (r *GitClonerTemplate) Run(ctx context.Context, command string, workDir string) (*ports.CommandResultDTO, error) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	cmd.Dir = workDir

	output, err := cmd.CombinedOutput()

	result := &ports.CommandResultDTO{
		Output:   string(output),
		ExitCode: 0,
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}
		return nil, err
	}

	return result, nil
}
