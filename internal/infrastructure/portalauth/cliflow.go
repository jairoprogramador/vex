package portalauth

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"
)

// CLIFlowConfig packages the OAuth Device Authorization Grant flow as a
// reusable, side-effect-aware unit. It encapsulates the bits that both
// `vex auth login` and the remote-executor's auto-login share: kicking off
// the device-code request, surfacing the verification URL/code to the user,
// best-effort opening the browser, polling for approval, and persisting the
// resulting token via TokenStore.
//
// What is intentionally NOT here:
//
//   - Post-login UX (e.g. printing whoami, exit codes). Callers decide how
//     to present success because that varies between the standalone auth
//     subcommand and the remote-executor pre-flight.
//   - Error display. Sentinel errors (ErrAccessDenied, ErrDeviceCodeExpired,
//     context.Canceled) and wrapped transport errors propagate unchanged so
//     each caller can map them to its own messages and exit codes.
//
// Callers must supply Client and Store; Stdout/Stderr/Clock/OpenBrowser
// fall back to sensible defaults when nil. OnWaiting is fully optional and
// powers a progress indicator that lives only as long as Poll is running.
type CLIFlowConfig struct {
	Client *DeviceFlowClient
	Store  TokenStore

	// Stdout receives user-facing instructions (verification URL, code,
	// "Waiting for approval..."). Defaults to os.Stdout.
	Stdout io.Writer
	// Stderr receives non-fatal warnings (browser open failure). Defaults
	// to os.Stderr.
	Stderr io.Writer
	// Clock backs the timestamps stamped on the persisted token.
	// Defaults to time.Now().UTC.
	Clock func() time.Time
	// OpenBrowser launches the verification URL. Defaults to OpenBrowser.
	// Failures are non-fatal: the URL is already on Stdout for manual use.
	OpenBrowser func(string) error
	// OnWaiting is invoked once Poll starts. The returned stop function is
	// called when Poll completes (regardless of outcome). Use it to drive
	// a progress indicator. When nil, Run prints a single-line
	// "Waiting for approval..." message instead.
	OnWaiting func(ctx context.Context) (stop func())

	// pollIntervalOverride bypasses the seconds-based interval derivation
	// when set. It exists exclusively for tests in this package, which need
	// sub-second polling cadence to stay fast. Production callers must
	// leave it at the zero value.
	pollIntervalOverride time.Duration
}

// Run drives the device flow end-to-end and returns the persisted Token on
// success. It is the single entry point callers should use; the helpers
// below stay private so the staged structure cannot drift between callers.
func (c CLIFlowConfig) Run(ctx context.Context) (Token, error) {
	c.applyDefaults()

	device, err := c.Client.Start(ctx)
	if err != nil {
		return Token{}, fmt.Errorf("start device flow: %w", err)
	}

	c.announceVerification(device)
	c.tryOpenBrowser(device.VerificationURIComplete)

	interval := time.Duration(device.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if c.pollIntervalOverride > 0 {
		interval = c.pollIntervalOverride
	}

	stopProgress := c.startWaitingIndicator(ctx)
	tokenResp, err := c.Client.Poll(ctx, device.DeviceCode, interval)
	stopProgress()

	if err != nil {
		// Sentinel and context errors propagate verbatim so each caller
		// can render the appropriate message and exit code.
		return Token{}, err
	}

	now := c.Clock()
	token := Token{
		AccessToken: tokenResp.AccessToken,
		TokenType:   tokenResp.TokenType,
		ObtainedAt:  now,
		ExpiresAt:   now.Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}
	if err := c.Store.Save(token); err != nil {
		return Token{}, fmt.Errorf("persist token: %w", err)
	}
	return token, nil
}

// applyDefaults swaps zero-value fields for production-safe defaults. It
// only touches optional fields; Client and Store are required and the
// caller is expected to validate them via the type system.
func (c *CLIFlowConfig) applyDefaults() {
	if c.Stdout == nil {
		c.Stdout = os.Stdout
	}
	if c.Stderr == nil {
		c.Stderr = os.Stderr
	}
	if c.Clock == nil {
		c.Clock = func() time.Time { return time.Now().UTC() }
	}
	if c.OpenBrowser == nil {
		c.OpenBrowser = OpenBrowser
	}
}

// announceVerification prints the URL/code lines that the end user needs
// in order to approve the device. Format mirrors what `vex auth login`
// has shipped since M2 so existing screenshots/docs stay accurate.
func (c CLIFlowConfig) announceVerification(device DeviceCodeResponse) {
	fmt.Fprintf(c.Stdout, "Open the following URL in your browser: %s", device.VerificationURIComplete)
}

// tryOpenBrowser opens the URL best-effort. Failures are surfaced on
// Stderr as a hint and never abort the flow — the URL is already on
// Stdout for manual copy/paste.
func (c CLIFlowConfig) tryOpenBrowser(url string) {
	if err := c.OpenBrowser(url); err != nil {
		fmt.Fprintf(c.Stderr, "(could not open browser automatically: %v)\n", err)
	}
}

// startWaitingIndicator spins up the OnWaiting indicator (if any) or
// prints a single-line fallback. It always returns a non-nil stop
// function so callers can defer it unconditionally.
func (c CLIFlowConfig) startWaitingIndicator(ctx context.Context) func() {
	if c.OnWaiting != nil {
		return c.OnWaiting(ctx)
	}
	fmt.Fprintln(c.Stdout, " Waiting for approval...")
	return func() {}
}
