package portalauth

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type loginRig struct {
	portal  *fakePortal
	store   *FileTokenStore
	browser *stubBrowser
	stdout  bytes.Buffer
	login   Login
}

func newLoginRig(t *testing.T, portalURL string) *loginRig {
	t.Helper()
	r := &loginRig{
		store:   NewFileTokenStoreAt(filepath.Join(t.TempDir(), "credentials.json")),
		browser: &stubBrowser{},
	}
	return r.with(t, portalURL)
}

func (r *loginRig) with(t *testing.T, portalURL string) *loginRig {
	t.Helper()
	r.portal = newFakePortal(t, writeDeviceCodeOK(), writeTokenSuccess())
	r.login = Login{
		Flow: CLIFlowConfig{
			Client: NewDeviceFlowClient(r.portal.server.URL), Store: r.store,
			Stdout: &r.stdout, Stderr: &bytes.Buffer{},
			PortalURL: portalURL, OpenBrowser: r.browser.open,
			pollIntervalOverride: time.Millisecond,
		},
		Notice: "Necesitas iniciar sesión.",
		Done:   "Sesión iniciada.",
	}
	return r
}

func TestLogin_EnsureToken_ConSesionVigenteNoHaceNada(t *testing.T) {
	t.Parallel()
	r := newLoginRig(t, "https://portal")
	now := time.Now()
	if err := r.store.Save(Token{AccessToken: "vigente", ExpiresAt: now.Add(time.Hour), ObtainedAt: now}); err != nil {
		t.Fatal(err)
	}

	if err := r.login.EnsureToken(context.Background()); err != nil {
		t.Fatalf("EnsureToken: %v", err)
	}

	if r.browser.called.Load() != 0 || r.portal.tokenHits.Load() != 0 {
		t.Fatal("with a valid session neither the browser nor the portal is touched")
	}
	if r.stdout.Len() != 0 {
		t.Fatalf("nothing to announce: %q", r.stdout.String())
	}
}

func TestLogin_EnsureToken_SinSesionAbreElNavegadorYGuardaElToken(t *testing.T) {
	t.Parallel()
	r := newLoginRig(t, "https://portal")

	if err := r.login.EnsureToken(context.Background()); err != nil {
		t.Fatalf("EnsureToken: %v", err)
	}

	stored, err := r.store.Load()
	if err != nil || stored.AccessToken != "tok-xyz" {
		t.Fatalf("stored token: %+v, %v", stored, err)
	}
	if r.browser.called.Load() != 1 || r.browser.gotURL.Load() != "https://portal/cli" {
		t.Fatalf("browser: called %d with %v", r.browser.called.Load(), r.browser.gotURL.Load())
	}
	out := r.stdout.String()
	notice, done := strings.Index(out, "Necesitas iniciar sesión."), strings.Index(out, "Sesión iniciada.")
	if notice != 0 || done < notice {
		t.Fatalf("the notice goes first and the confirmation last: %q", out)
	}
	if strings.Contains(out, "tok-xyz") {
		t.Fatal("the access token must never be printed")
	}
}

func TestLogin_EnsureToken_UnTokenVencidoVuelveAIniciarSesion(t *testing.T) {
	t.Parallel()
	r := newLoginRig(t, "https://portal")
	past := time.Now().Add(-time.Hour)
	if err := r.store.Save(Token{AccessToken: "vencido", ExpiresAt: past, ObtainedAt: past.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}

	if err := r.login.EnsureToken(context.Background()); err != nil {
		t.Fatalf("EnsureToken: %v", err)
	}

	if stored, _ := r.store.Load(); stored.AccessToken != "tok-xyz" {
		t.Fatalf("the expired token must be replaced: %+v", stored)
	}
}

func TestLogin_EnsureToken_SoloAbreURLsDelPortal(t *testing.T) {
	t.Parallel()
	// The fake portal answers with https://portal/cli; the configured portal is another origin.
	r := newLoginRig(t, "https://otro-portal.example")

	if err := r.login.EnsureToken(context.Background()); err != nil {
		t.Fatalf("EnsureToken: %v", err)
	}

	if r.browser.called.Load() != 0 {
		t.Fatal("a verification URL outside the configured portal must not be opened")
	}
}

func TestLogin_EnsureToken_ConservaLosErroresCentinela(t *testing.T) {
	t.Parallel()
	r := newLoginRig(t, "https://portal")
	r.portal.tokenCases = []http.HandlerFunc{writeTokenError("access_denied")}

	err := r.login.EnsureToken(context.Background())

	if !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("got %v", err)
	}
	if _, loadErr := r.store.Load(); !errors.Is(loadErr, ErrTokenNotFound) {
		t.Fatal("a failed login stores nothing")
	}
	if strings.Contains(r.stdout.String(), "Sesión iniciada.") {
		t.Fatal("no confirmation after a failed login")
	}
}

func TestLogin_ForgetToken(t *testing.T) {
	t.Parallel()
	r := newLoginRig(t, "https://portal")
	if err := r.store.Save(Token{AccessToken: "x"}); err != nil {
		t.Fatal(err)
	}

	if err := r.login.ForgetToken(); err != nil {
		t.Fatalf("ForgetToken: %v", err)
	}
	if _, err := r.store.Load(); !errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("the token must be gone: %v", err)
	}
	if err := r.login.ForgetToken(); err != nil {
		t.Fatalf("forgetting twice is fine: %v", err)
	}
}
