package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jairoprogramador/vex/internal/infrastructure/factories"
	"github.com/jairoprogramador/vex/internal/infrastructure/portalauth"
	"github.com/spf13/cobra"
)

// authCmd is the parent of `login`, `logout` and `whoami`. It only acts as
// a grouping command — the real work happens in the subcommands.
var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authenticate vex against the Vex portal.",
	Long:  "Manage portal credentials for the vex CLI using the OAuth Device Authorization Grant flow.",
}

// authLoginCmd starts the Device Code flow against the portal and persists
// the resulting access token to the local credentials file.
var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate vex against the portal via browser.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAuthLogin(cmd.Context())
	},
}

// authLogoutCmd removes the local credentials file. Server-side revocation
// is intentionally out of scope until M6+.
var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Remove locally stored credentials.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAuthLogout(cmd.Context())
	},
}

// authWhoamiCmd reports the identity associated with the currently stored
// access token by hitting the `whoami` edge function.
var authWhoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show the user identity bound to the current credentials.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAuthWhoami(cmd.Context())
	},
}

func init() {
	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authLogoutCmd)
	authCmd.AddCommand(authWhoamiCmd)
}

// runAuthLogin orchestrates the user-facing portion of the device flow:
// kick off the request, surface the verification URL/code, open the browser
// (best-effort), poll until approval, persist the token, and finish with a
// whoami greeting. The shared device-flow plumbing lives in
// portalauth.CLIFlowConfig — this function only contributes the auth-only
// UX (progress dots, custom exit codes for sentinel errors, whoami after
// success).
func runAuthLogin(parentCtx context.Context) error {
	ctx, stop := signal.NotifyContext(parentCtx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	deps, err := factories.NewServiceFactory().BuildAuth()
	if err != nil {
		return fmt.Errorf("auth login: %w", err)
	}

	flow := portalauth.CLIFlowConfig{
		Client: deps.DeviceClient,
		Store:  deps.TokenStore,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		// Progress UX specific to the interactive auth subcommand: a dot
		// every 5s prefixed by an inline "Waiting for approval" line and a
		// trailing newline once polling stops.
		OnWaiting: func(ctx context.Context) func() {
			fmt.Fprint(os.Stdout, "Waiting for approval")
			stopProgress := startProgressDots(ctx)
			return func() {
				stopProgress()
				fmt.Fprintln(os.Stdout)
			}
		},
	}

	token, err := flow.Run(ctx)
	switch {
	case errors.Is(err, context.Canceled):
		fmt.Fprintln(os.Stderr, "Login canceled.")
		os.Exit(130)
	case errors.Is(err, portalauth.ErrAccessDenied):
		fmt.Fprintln(os.Stderr, "Access denied: the portal rejected this device. Run 'vex auth login' to retry.")
		os.Exit(1)
	case errors.Is(err, portalauth.ErrDeviceCodeExpired):
		fmt.Fprintln(os.Stderr, "Device code expired before approval. Run 'vex auth login' to retry.")
		os.Exit(1)
	case err != nil:
		return fmt.Errorf("auth login: %w", err)
	}

	identity, err := fetchWhoami(ctx, deps.HTTPClient, deps.PortalURL, token.AccessToken)
	if err != nil {
		// The token is already saved; surface the warning but consider
		// login successful so the user can keep using the CLI.
		fmt.Fprintf(os.Stderr, "(warning: could not verify identity: %v)\n", err)
		fmt.Fprintln(os.Stdout, "Authenticated.")
		return nil
	}

	fmt.Fprintf(os.Stdout, "Authenticated as %s\n", identity.Email)
	return nil
}

// runAuthLogout deletes the local credentials file. The operation is
// idempotent and the server-side token remains valid until expiration.
func runAuthLogout(_ context.Context) error {
	deps, err := factories.NewServiceFactory().BuildAuth()
	if err != nil {
		return fmt.Errorf("auth logout: %w", err)
	}
	if err := deps.TokenStore.Delete(); err != nil {
		return fmt.Errorf("delete credentials: %w", err)
	}
	fmt.Fprintln(os.Stdout, "Local credentials cleared.")
	fmt.Fprintln(os.Stdout, "Note: the token may still be valid on the server until expiration. "+
		"Use 'vex auth revoke' (TODO M6+) to revoke server-side.")
	return nil
}

// runAuthWhoami loads the persisted token and resolves the identity via
// the portal's whoami endpoint.
func runAuthWhoami(parentCtx context.Context) error {
	ctx, stop := signal.NotifyContext(parentCtx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	deps, err := factories.NewServiceFactory().BuildAuth()
	if err != nil {
		return fmt.Errorf("auth whoami: %w", err)
	}

	token, err := deps.TokenStore.Load()
	if err != nil {
		if errors.Is(err, portalauth.ErrTokenNotFound) {
			fmt.Fprintln(os.Stderr, "Not authenticated. Run 'vex auth login'.")
			os.Exit(1)
		}
		return fmt.Errorf("load credentials: %w", err)
	}

	identity, err := fetchWhoami(ctx, deps.HTTPClient, deps.PortalURL, token.AccessToken)
	if err != nil {
		var unauth unauthorizedError
		if errors.As(err, &unauth) {
			fmt.Fprintln(os.Stderr, "Token expired or revoked. Run 'vex auth login'.")
			os.Exit(1)
		}
		return fmt.Errorf("call whoami: %w", err)
	}

	fmt.Fprintf(os.Stdout, "User ID:  %s\n", identity.UserID)
	fmt.Fprintf(os.Stdout, "Email:    %s\n", identity.Email)
	fmt.Fprintf(os.Stdout, "Token ID: %s\n", identity.TokenID)
	return nil
}

// whoamiResponse mirrors the body of the `whoami` edge function (§6.3).
type whoamiResponse struct {
	UserID  string `json:"user_id"`
	Email   string `json:"email"`
	TokenID string `json:"token_id"`
}

// unauthorizedError signals a 401 from the portal and lets callers branch
// on it via errors.As without inspecting status codes directly.
type unauthorizedError struct{ status int }

func (e unauthorizedError) Error() string {
	return fmt.Sprintf("whoami: unauthorized (status %d)", e.status)
}

// fetchWhoami calls GET /functions/v1/whoami with a Bearer token and
// decodes the response. A 401 is mapped to unauthorizedError.
func fetchWhoami(ctx context.Context, client *http.Client, portalURL, accessToken string) (whoamiResponse, error) {
	url := strings.TrimRight(portalURL, "/") + "/functions/v1/whoami"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return whoamiResponse{}, fmt.Errorf("build whoami request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return whoamiResponse{}, fmt.Errorf("call whoami: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return whoamiResponse{}, unauthorizedError{status: resp.StatusCode}
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return whoamiResponse{}, fmt.Errorf("whoami: unexpected status %d: %s",
			resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var out whoamiResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return whoamiResponse{}, fmt.Errorf("decode whoami response: %w", err)
	}
	return out, nil
}

// startProgressDots prints a dot to stdout every 5 seconds until the
// returned cancel function is invoked or ctx is canceled. It is a
// deliberately tiny UX helper so the user sees that the CLI is alive.
func startProgressDots(ctx context.Context) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-ticker.C:
				fmt.Fprint(os.Stdout, ".")
			}
		}
	}()
	return func() {
		select {
		case <-stop:
		default:
			close(stop)
		}
		<-done
	}
}
