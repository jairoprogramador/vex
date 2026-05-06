package portalauth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakePortal builds an httptest.Server that responds to the two device-flow
// endpoints with the configured payloads. It is local to this file so the
// public DeviceFlowClient tests stay decoupled from the helper's tests.
type fakePortal struct {
	server      *httptest.Server
	tokenHits   atomic.Int32
	tokenCases  []http.HandlerFunc
	deviceCase  http.HandlerFunc
}

func newFakePortal(t *testing.T, deviceCase http.HandlerFunc, tokenCases ...http.HandlerFunc) *fakePortal {
	t.Helper()
	p := &fakePortal{deviceCase: deviceCase, tokenCases: tokenCases}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/functions/v1/device-code":
			p.deviceCase(w, r)
		case "/functions/v1/device-token":
			idx := int(p.tokenHits.Add(1)) - 1
			if idx >= len(p.tokenCases) {
				t.Errorf("device-token hit %d times, only %d responses configured", idx+1, len(p.tokenCases))
				http.Error(w, "no response configured", http.StatusInternalServerError)
				return
			}
			p.tokenCases[idx](w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(p.server.Close)
	return p
}

// stubBrowser captures the URL passed to OpenBrowser so tests can assert
// the helper attempted to open the verification URL even when the
// underlying call returned an error.
type stubBrowser struct {
	called atomic.Int32
	gotURL atomic.Value // string
	err    error
}

func (s *stubBrowser) open(url string) error {
	s.called.Add(1)
	s.gotURL.Store(url)
	return s.err
}

func writeDeviceCodeOK() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{
            "device_code": "dev-1",
            "user_code": "WDJB-MJHT",
            "verification_uri": "https://portal/cli",
            "verification_uri_complete": "https://portal/cli?code=WDJB-MJHT",
            "expires_in": 600,
            "interval": 0
        }`)
	}
}

func writeTokenSuccess() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"access_token":"tok-xyz","token_type":"Bearer","expires_in":3600}`)
	}
}

func writeTokenError(errCode string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, `{"error":%q}`, errCode)
	}
}

func TestCLIFlowConfig_Run(t *testing.T) {
	t.Parallel()

	t.Run("happy path persists token and announces verification URL", func(t *testing.T) {
		t.Parallel()

		portal := newFakePortal(t,
			writeDeviceCodeOK(),
			writeTokenSuccess(),
		)
		client := NewDeviceFlowClient(portal.server.URL)
		store := NewFileTokenStoreAt(filepath.Join(t.TempDir(), "credentials.json"))
		browser := &stubBrowser{}
		var stdout, stderr bytes.Buffer
		fixedClock := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)

		cfg := CLIFlowConfig{
			Client:               client,
			Store:                store,
			Stdout:               &stdout,
			Stderr:               &stderr,
			Clock:                func() time.Time { return fixedClock },
			OpenBrowser:          browser.open,
			pollIntervalOverride: 1 * time.Millisecond,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		token, err := cfg.Run(ctx)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}

		if token.AccessToken != "tok-xyz" {
			t.Fatalf("access token: got %q", token.AccessToken)
		}
		if !token.ObtainedAt.Equal(fixedClock) {
			t.Fatalf("obtained_at: got %v want %v", token.ObtainedAt, fixedClock)
		}
		if want := fixedClock.Add(time.Hour); !token.ExpiresAt.Equal(want) {
			t.Fatalf("expires_at: got %v want %v", token.ExpiresAt, want)
		}

		// Token must round-trip via the store too.
		stored, err := store.Load()
		if err != nil {
			t.Fatalf("store.Load: %v", err)
		}
		if stored.AccessToken != "tok-xyz" {
			t.Fatalf("stored token: got %q", stored.AccessToken)
		}

		if browser.called.Load() != 1 {
			t.Fatalf("browser called: got %d", browser.called.Load())
		}
		if got, _ := browser.gotURL.Load().(string); got != "https://portal/cli?code=WDJB-MJHT" {
			t.Fatalf("browser URL: got %q", got)
		}

		out := stdout.String()
		if !strings.Contains(out, "Open the following URL") {
			t.Fatalf("missing announcement: %q", out)
		}
		if !strings.Contains(out, "WDJB-MJHT") {
			t.Fatalf("missing user code: %q", out)
		}
		if !strings.Contains(out, "Waiting for approval") {
			t.Fatalf("missing waiting line: %q", out)
		}
	})

	t.Run("browser open failure surfaces on stderr but does not abort", func(t *testing.T) {
		t.Parallel()

		portal := newFakePortal(t, writeDeviceCodeOK(), writeTokenSuccess())
		store := NewFileTokenStoreAt(filepath.Join(t.TempDir(), "credentials.json"))
		var stderr bytes.Buffer
		browser := &stubBrowser{err: errors.New("xdg-open missing")}

		cfg := CLIFlowConfig{
			Client:               NewDeviceFlowClient(portal.server.URL),
			Store:                store,
			Stderr:               &stderr,
			OpenBrowser:          browser.open,
			pollIntervalOverride: 1 * time.Millisecond,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if _, err := cfg.Run(ctx); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if !strings.Contains(stderr.String(), "could not open browser automatically") {
			t.Fatalf("expected browser failure on stderr: %q", stderr.String())
		}
	})

	t.Run("access_denied propagates verbatim", func(t *testing.T) {
		t.Parallel()

		portal := newFakePortal(t,
			writeDeviceCodeOK(),
			writeTokenError("access_denied"),
		)
		store := NewFileTokenStoreAt(filepath.Join(t.TempDir(), "credentials.json"))

		cfg := CLIFlowConfig{
			Client:               NewDeviceFlowClient(portal.server.URL),
			Store:                store,
			OpenBrowser:          func(string) error { return nil },
			pollIntervalOverride: 1 * time.Millisecond,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_, err := cfg.Run(ctx)
		if !errors.Is(err, ErrAccessDenied) {
			t.Fatalf("expected ErrAccessDenied, got %v", err)
		}
	})

	t.Run("OnWaiting indicator is started and stopped exactly once", func(t *testing.T) {
		t.Parallel()

		portal := newFakePortal(t, writeDeviceCodeOK(), writeTokenSuccess())
		store := NewFileTokenStoreAt(filepath.Join(t.TempDir(), "credentials.json"))

		var starts, stops atomic.Int32
		cfg := CLIFlowConfig{
			Client:               NewDeviceFlowClient(portal.server.URL),
			Store:                store,
			OpenBrowser:          func(string) error { return nil },
			pollIntervalOverride: 1 * time.Millisecond,
			OnWaiting: func(ctx context.Context) func() {
				starts.Add(1)
				return func() { stops.Add(1) }
			},
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if _, err := cfg.Run(ctx); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if got := starts.Load(); got != 1 {
			t.Fatalf("OnWaiting started: got %d, want 1", got)
		}
		if got := stops.Load(); got != 1 {
			t.Fatalf("OnWaiting stop: got %d, want 1", got)
		}
	})
}
