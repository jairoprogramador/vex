package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jairoprogramador/vex/internal/infrastructure/factories"
	"github.com/jairoprogramador/vex/internal/infrastructure/portalauth"
	"github.com/jairoprogramador/vex/internal/infrastructure/portalclient"
	"github.com/spf13/cobra"
)

// executionCancelCmd asks the portal to cancel an in-flight execution.
// The portal handles Fly Machine teardown asynchronously (§6.9).
var cancelCmd = &cobra.Command{
	Use:   "cancel <execution-id>",
	Short: "Cancel a running execution.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCancel(cmd.Context(), args[0])
	},
}

// runCancel wires the portal client and forwards the cancel
// request. Errors are translated to user-friendly messages for the common
// cases (no token, expired token, not found).
func runCancel(parentCtx context.Context, executionID string) error {
	ctx, stop := signal.NotifyContext(parentCtx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	client, err := factories.NewServiceFactory().BuildPortalClient()
	if err != nil {
		return fmt.Errorf("execution cancel: %w", err)
	}

	if err := client.CancelExecution(ctx, executionID); err != nil {
		switch {
		case errors.Is(err, portalauth.ErrTokenNotFound):
			fmt.Fprintln(os.Stderr, "Not authenticated. Run 'vex login' first.")
			os.Exit(1)
		case errors.Is(err, portalclient.ErrUnauthorized):
			fmt.Fprintln(os.Stderr, "Portal rejected the saved credentials. Run 'vex login' to refresh.")
			os.Exit(1)
		case errors.Is(err, portalclient.ErrNotFound):
			return fmt.Errorf("execution %s not found (already finished, never existed, or not yours)", executionID)
		case errors.Is(err, portalclient.ErrForbidden):
			return errors.New("portal denied access to this execution (membership required)")
		case errors.Is(err, portalclient.ErrConflict):
			return fmt.Errorf("execution %s is already in a terminal state (cannot cancel)", executionID)
		case errors.Is(err, portalclient.ErrGlobalCapacityReached):
			// User-initiated cancel: surface the hint and let the user
			// decide when to retry. We don't auto-retry like --follow does.
			var httpErr *portalclient.HTTPError
			if errors.As(err, &httpErr) && httpErr.RetryAfter > 0 {
				return fmt.Errorf("portal busy, retry in %ds", httpErr.RetryAfter)
			}
			return errors.New("portal busy, retry shortly")
		}
		return fmt.Errorf("cancel execution: %w", err)
	}

	fmt.Fprintf(os.Stdout, "Execution %s canceled.\n", executionID)
	return nil
}
