package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/jairoprogramador/vex/internal/domain/project/ports"
)

const defaultTimeout = 3 * time.Second

type ShellGitInfo struct {
	timeout time.Duration
}

func NewShellGitInfo() ports.GitInfo {
	return &ShellGitInfo{timeout: defaultTimeout}
}

func (g *ShellGitInfo) RemoteURL(ctx context.Context, dir string) (string, error) {
	out, err := g.run(ctx, dir, "git", "remote", "get-url", "origin")
	if err != nil {
		return "", fmt.Errorf("git remote get-url origin: %w", err)
	}
	return strings.TrimSpace(out), nil
}

func (g *ShellGitInfo) CurrentRef(ctx context.Context, dir string) (string, error) {
	out, err := g.run(ctx, dir, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse --abbrev-ref HEAD: %w", err)
	}
	return strings.TrimSpace(out), nil
}

func (g *ShellGitInfo) run(ctx context.Context, dir, name string, args ...string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, name, args...)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return "", err
	}
	return stdout.String(), nil
}
