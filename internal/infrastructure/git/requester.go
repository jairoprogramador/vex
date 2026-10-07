package git

import (
	"context"
	"os/user"
	"strings"
	"time"
)

const (
	requesterTimeout = 3 * time.Second
	unknownRequester = "vex"
)

// RequesterName es quien pide la ejecución: el usuario de git, o el del sistema si
// git no lo tiene configurado. Nunca falla: el historial del motor solo lo guarda.
func RequesterName(ctx context.Context, dir string) string {
	ctx, cancel := context.WithTimeout(ctx, requesterTimeout)
	defer cancel()

	if out, err := runGit(ctx, dir, "config", "user.name"); err == nil {
		if name := strings.TrimSpace(out); name != "" {
			return name
		}
	}
	if current, err := user.Current(); err == nil && current.Username != "" {
		return current.Username
	}
	return unknownRequester
}
