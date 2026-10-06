package git

import (
	"context"
	"fmt"
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
	out, err := g.run(ctx, dir, "remote", "get-url", "origin")
	if err != nil {
		return "", fmt.Errorf("git remote get-url origin: %w", err)
	}
	return strings.TrimSpace(out), nil
}

func (g *ShellGitInfo) CurrentRef(ctx context.Context, dir string) (string, error) {
	out, err := g.run(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse --abbrev-ref HEAD: %w", err)
	}
	return strings.TrimSpace(out), nil
}

func (g *ShellGitInfo) run(ctx context.Context, dir string, args ...string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	return runGit(runCtx, dir, args...)
}
