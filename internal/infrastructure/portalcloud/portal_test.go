package portalcloud

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jairoprogramador/vex/internal/application"
	"github.com/jairoprogramador/vex/internal/infrastructure/portalauth"
	"github.com/jairoprogramador/vex/internal/infrastructure/portalclient"
)

// fixture lee las respuestas de ejemplo del cliente del portal: son el mismo contrato.
func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "portalclient", "testdata", "cloud", name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return string(data)
}

func seededStore(t *testing.T) portalauth.TokenStore {
	t.Helper()
	store := portalauth.NewFileTokenStoreAt(filepath.Join(t.TempDir(), "credentials.json"))
	err := store.Save(portalauth.Token{
		AccessToken: "test-token", TokenType: "Bearer",
		ObtainedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("sembrar el token: %v", err)
	}
	return store
}

// newPortal levanta un portal falso que responde siempre lo mismo.
func newPortal(t *testing.T, status int, header http.Header, body string) *Portal {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for k, vs := range header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return New(portalclient.NewPortalClient(srv.URL, seededStore(t), srv.Client()))
}

func freshToken() string {
	now := time.Now().UTC()
	return fmt.Sprintf(`{"token":"eyJhbGciOiJSUzI1NiJ9.e30.c2ln","expires_at":%q,"refresh_after":%q}`,
		now.Add(10*time.Minute).Format(time.RFC3339), now.Add(5*time.Minute).Format(time.RFC3339))
}

func TestCloudConnection_ActivaLlevaLosOutputsSinInterpretar(t *testing.T) {
	t.Parallel()
	portal := newPortal(t, http.StatusOK, nil, fixture(t, "connection_active.json"))

	info, err := portal.CloudConnection(context.Background(), "proj-1", "sand")
	if err != nil {
		t.Fatalf("CloudConnection: %v", err)
	}

	if info.Status != "active" || info.ConnectionID != "5b0c8a52-3f0e-4d55-9a6e-0f6d0e1c2a11" ||
		info.BootstrapSHA256 != "be9cd7fb3cf30f9800b778e31b0cc34d3bd9c89907dc158b4079dc7644dcf806" ||
		info.PipelineCommit != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("got %+v", info)
	}
	if info.Reason != "" || info.Hint != "" {
		t.Fatalf("reason/hint nulos deben quedar vacíos: %q / %q", info.Reason, info.Hint)
	}
	if info.Outputs == nil || info.Outputs.ContractID != "azure/v1" || info.Outputs.ContractID.Cloud() != "azure" {
		t.Fatalf("outputs: %+v", info.Outputs)
	}
	if len(info.Outputs.Params) != 12 || info.Outputs.Params["state_container"] != "tfstate" {
		t.Fatalf("params: %v", info.Outputs.Params)
	}
	if len(info.Outputs.Extras) != 1 || info.Outputs.Extras["registryLoginServer"] != "crvexdemosand.azurecr.io" {
		t.Fatalf("extras: %v", info.Outputs.Extras)
	}
}

func TestCloudConnection_SinConexionUtilizable(t *testing.T) {
	t.Parallel()

	cases := []struct{ fixture, status, reason, hint string }{
		{"connection_absent.json", "absent", "no_connection", "vex bootstrap sand"},
		{"connection_revoked.json", "revoked", "revoked", "vex bootstrap sand"},
		{"connection_failed.json", "failed", "verify_failed", "vex bootstrap sand --reconnect"},
	}
	for _, c := range cases {
		t.Run(c.status, func(t *testing.T) {
			t.Parallel()
			portal := newPortal(t, http.StatusOK, nil, fixture(t, c.fixture))

			info, err := portal.CloudConnection(context.Background(), "proj-1", "sand")
			if err != nil {
				t.Fatalf("CloudConnection: %v", err)
			}
			if info.Status != c.status || info.Reason != c.reason || info.Hint != c.hint || info.Outputs != nil {
				t.Fatalf("got %+v", info)
			}
		})
	}
}

func TestIssue_DevuelveElTokenConSusTiempos(t *testing.T) {
	t.Parallel()
	portal := newPortal(t, http.StatusOK, nil, freshToken())

	token, err := portal.Issue(context.Background(), application.TokenRequest{ProjectID: "proj-1", Environment: "sand"})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if token.Value != "eyJhbGciOiJSUzI1NiJ9.e30.c2ln" {
		t.Fatal("el valor del token no llegó")
	}
	if !token.RefreshAfter.Before(token.ExpiresAt) || !token.ExpiresAt.After(time.Now()) {
		t.Fatalf("tiempos: %v", token)
	}
}

func TestIssue_ClasificaLosErrores(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		status    int
		fixture   string
		original  error
		denied    bool
		throttled bool
	}{
		{name: "401", status: http.StatusUnauthorized, fixture: "error_invalid_token.json",
			original: portalclient.ErrUnauthorized, denied: true},
		{name: "403", status: http.StatusForbidden, fixture: "error_forbidden.json",
			original: portalclient.ErrForbidden, denied: true},
		{name: "409 conexión no activa", status: http.StatusConflict, fixture: "error_connection_not_active.json",
			original: portalclient.ErrConnectionNotActive, denied: true},
		{name: "429 límite de emisiones", status: http.StatusTooManyRequests, fixture: "error_rate_limited.json",
			original: portalclient.ErrRateLimited, throttled: true},
		{name: "502 firma", status: http.StatusBadGateway, fixture: "error_signing_unavailable.json",
			original: portalclient.ErrSigningUnavailable},
		{name: "503 auditoría", status: http.StatusServiceUnavailable, fixture: "error_audit_unavailable.json",
			original: portalclient.ErrAuditUnavailable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			portal := newPortal(t, c.status, http.Header{"Retry-After": []string{"3600"}}, fixture(t, c.fixture))

			token, err := portal.Issue(context.Background(), application.TokenRequest{ProjectID: "proj-1", Environment: "sand"})

			if token.Value != "" {
				t.Fatal("un error no devuelve token")
			}
			if !errors.Is(err, c.original) {
				t.Fatalf("se perdió el error original %v: %v", c.original, err)
			}
			if got := errors.Is(err, application.ErrCloudTokenDenied); got != c.denied {
				t.Fatalf("ErrCloudTokenDenied: got %v, want %v (%v)", got, c.denied, err)
			}
			if got := errors.Is(err, application.ErrCloudTokenThrottled); got != c.throttled {
				t.Fatalf("ErrCloudTokenThrottled: got %v, want %v (%v)", got, c.throttled, err)
			}
			var retry application.RetryAfter
			if got := errors.As(err, &retry); got != c.throttled {
				t.Fatalf("RetryAfter: presente=%v, want %v", got, c.throttled)
			}
			if c.throttled && retry.RetryAfter() != time.Hour {
				t.Fatalf("RetryAfter: got %v, want 1h", retry.RetryAfter())
			}
		})
	}
}

func TestIssue_LimiteSinRetryAfterEsperaCero(t *testing.T) {
	t.Parallel()
	portal := newPortal(t, http.StatusTooManyRequests, nil, fixture(t, "error_rate_limited.json"))

	_, err := portal.Issue(context.Background(), application.TokenRequest{ProjectID: "proj-1", Environment: "sand"})

	var retry application.RetryAfter
	if !errors.As(err, &retry) || retry.RetryAfter() != 0 {
		t.Fatalf("got %v", err)
	}
}

func TestCloudConnection_TambienClasificaLosErrores(t *testing.T) {
	t.Parallel()
	portal := newPortal(t, http.StatusUnauthorized, nil, fixture(t, "error_invalid_token.json"))

	_, err := portal.CloudConnection(context.Background(), "proj-1", "sand")

	if !errors.Is(err, application.ErrCloudTokenDenied) || !errors.Is(err, portalclient.ErrUnauthorized) {
		t.Fatalf("got %v", err)
	}
}

func TestSinSesionEsDefinitivoYNoTocaLaRed(t *testing.T) {
	t.Parallel()

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer srv.Close()
	empty := portalauth.NewFileTokenStoreAt(filepath.Join(t.TempDir(), "credentials.json"))
	portal := New(portalclient.NewPortalClient(srv.URL, empty, srv.Client()))

	_, err := portal.Issue(context.Background(), application.TokenRequest{ProjectID: "proj-1", Environment: "sand"})

	if !errors.Is(err, portalauth.ErrTokenNotFound) || !errors.Is(err, application.ErrCloudTokenDenied) {
		t.Fatalf("got %v", err)
	}
	if hits != 0 {
		t.Fatalf("se tocó la red %d veces", hits)
	}
}

func TestIssue_UnErrorNuncaReproduceUnToken(t *testing.T) {
	t.Parallel()

	const jwt = "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJzZWNyZXRvIn0.ZmlybWEtc2VjcmV0YQ"
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusInternalServerError} {
		portal := newPortal(t, status, nil, `{"error":"rate_limited","message":"token `+jwt+` rechazado"}`)

		_, err := portal.Issue(context.Background(), application.TokenRequest{ProjectID: "proj-1", Environment: "sand"})

		if err == nil {
			t.Fatalf("%d: se esperaba un error", status)
		}
		if shown := fmt.Sprintf("%v %+v", err, err); strings.Contains(shown, "eyJ") || strings.Contains(shown, "ZmlybWEtc2VjcmV0YQ") {
			t.Fatalf("%d: el error reproduce el token: %s", status, shown)
		}
	}
}

// Cada rechazo definitivo se arregla de una forma distinta: la aplicación tiene que poder distinguirlos.
func TestLosRechazosDefinitivosDicenCualSon(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		status         int
		fixture        string
		sessionInvalid bool
		connectionDown bool
	}{
		{"sesión vencida o revocada", http.StatusUnauthorized, "error_invalid_token.json", true, false},
		{"conexión que ya no está activa", http.StatusConflict, "error_connection_not_active.json", false, true},
		{"sin permiso en el proyecto", http.StatusForbidden, "error_forbidden.json", false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			portal := newPortal(t, c.status, nil, fixture(t, c.fixture))

			_, err := portal.Issue(context.Background(), application.TokenRequest{ProjectID: "proj-1", Environment: "sand"})

			if !errors.Is(err, application.ErrCloudTokenDenied) {
				t.Fatalf("debe ser definitivo: %v", err)
			}
			if got := errors.Is(err, application.ErrPortalSessionInvalid); got != c.sessionInvalid {
				t.Fatalf("ErrPortalSessionInvalid: got %v (%v)", got, err)
			}
			if got := errors.Is(err, application.ErrCloudConnectionInactive); got != c.connectionDown {
				t.Fatalf("ErrCloudConnectionInactive: got %v (%v)", got, err)
			}
		})
	}
}
