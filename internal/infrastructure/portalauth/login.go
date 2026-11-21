package portalauth

import (
	"context"
	"errors"
	"fmt"
)

// Login is the single place where the CLI signs the user in. Every command
// that needs a portal session (`vex login`, the remote executor and the
// local executor with `cloud.credentials: portal`) goes through it, so
// they all share the same device flow, the same browser rule (only the
// portal's own origin is opened automatically) and the same token store.
//
// Flow.Client and Flow.Store are required; the rest of Flow falls back to
// its defaults.
type Login struct {
	Flow CLIFlowConfig

	// Notice is printed on Flow.Stdout right before EnsureToken starts the
	// browser flow, so the user knows why a browser is about to open.
	Notice string
	// Done is printed on Flow.Stdout after EnsureToken signs the user in.
	Done string
}

// EnsureToken makes sure a usable session exists: it returns right away
// when a non-expired token is stored and drives the browser flow otherwise.
// A token the portal has revoked still looks usable here; callers that get
// a 401 call ForgetToken and then EnsureToken again.
//
// Sentinel errors (ErrAccessDenied, ErrDeviceCodeExpired, context.Canceled)
// stay reachable through errors.Is.
func (l Login) EnsureToken(ctx context.Context) error {
	token, err := l.Flow.Store.Load()
	switch {
	case err == nil && !token.Expired():
		return nil
	case err != nil && !errors.Is(err, ErrTokenNotFound):
		return fmt.Errorf("load credentials: %w", err)
	}

	l.Flow.applyDefaults()
	if l.Notice != "" {
		fmt.Fprintln(l.Flow.Stdout, l.Notice)
	}
	if _, err := l.Flow.Run(ctx); err != nil {
		return fmt.Errorf("device flow: %w", err)
	}
	if l.Done != "" {
		fmt.Fprintln(l.Flow.Stdout, l.Done)
	}
	return nil
}

// Run signs the user in unconditionally, replacing any stored token, and
// returns the new one. Errors propagate unchanged.
func (l Login) Run(ctx context.Context) (Token, error) {
	return l.Flow.Run(ctx)
}

// ForgetToken drops the stored token, typically after the portal rejected
// it. A missing token is not an error.
func (l Login) ForgetToken() error {
	if err := l.Flow.Store.Delete(); err != nil && !errors.Is(err, ErrTokenNotFound) {
		return fmt.Errorf("delete credentials: %w", err)
	}
	return nil
}
