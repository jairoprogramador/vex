package portalclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jairoprogramador/vex/internal/infrastructure/portalauth"
)

// fixtureNow is "a moment ago" for testdata/cloud/token_issued.json: the
// fixture token is still valid at this instant.
var fixtureNow = time.Date(2026, 7, 10, 15, 0, 0, 0, time.UTC)

func cloudFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "cloud", name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return data
}

// recordedRequest is what the fake portal saw.
type recordedRequest struct {
	method      string
	path        string
	rawQuery    string
	auth        string
	contentType string
	body        []byte
}

// newCloudServer answers every request with status, header and body, and
// records the last request it received.
func newCloudServer(t *testing.T, status int, header http.Header, body []byte) (*PortalClient, *recordedRequest) {
	t.Helper()
	seen := &recordedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, _ := io.ReadAll(r.Body)
		*seen = recordedRequest{
			method: r.Method, path: r.URL.Path, rawQuery: r.URL.RawQuery,
			auth: r.Header.Get("Authorization"), contentType: r.Header.Get("Content-Type"), body: payload,
		}
		w.Header().Set("Content-Type", "application/json")
		for k, vs := range header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	client := NewPortalClient(srv.URL, newTestStore(t), srv.Client())
	client.now = func() time.Time { return fixtureNow }
	return client, seen
}

func TestPortalClient_CloudConnection_Active(t *testing.T) {
	t.Parallel()
	client, seen := newCloudServer(t, http.StatusOK, nil, cloudFixture(t, "connection_active.json"))

	resp, err := client.CloudConnection(context.Background(), "9d8d4777-5707-43b3-a636-d6f8433b7a34", "sand")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if seen.method != http.MethodGet || seen.path != "/functions/v1/cli-cloud-connection" {
		t.Fatalf("request: got %s %s", seen.method, seen.path)
	}
	if want := "environment=sand&project_id=9d8d4777-5707-43b3-a636-d6f8433b7a34"; seen.rawQuery != want {
		t.Fatalf("query: got %q, want %q", seen.rawQuery, want)
	}
	if seen.auth != "Bearer test-token" {
		t.Fatalf("authorization: got %q", seen.auth)
	}
	if seen.contentType != "" || len(seen.body) != 0 {
		t.Fatalf("a GET carries no body: content-type %q, body %q", seen.contentType, seen.body)
	}

	if resp.Status != "active" || resp.ConnectionID != "5b0c8a52-3f0e-4d55-9a6e-0f6d0e1c2a11" {
		t.Fatalf("response: got %+v", resp)
	}
	if resp.BootstrapSHA256 != "be9cd7fb3cf30f9800b778e31b0cc34d3bd9c89907dc158b4079dc7644dcf806" ||
		resp.PipelineCommit != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("bootstrap fingerprint: got %q @ %q", resp.BootstrapSHA256, resp.PipelineCommit)
	}
	if resp.Reason != "" || resp.Hint != "" {
		t.Fatalf("null reason/hint must decode to empty: %q / %q", resp.Reason, resp.Hint)
	}
	if resp.Outputs == nil || resp.Outputs.ContractID != "azure/v1" {
		t.Fatalf("outputs: got %+v", resp.Outputs)
	}
	// Params are opaque: whatever the portal sent must arrive untouched.
	var raw struct {
		Outputs struct {
			Params map[string]string `json:"params"`
		} `json:"outputs"`
	}
	if err := json.Unmarshal(cloudFixture(t, "connection_active.json"), &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw.Outputs.Params) != 12 || len(resp.Outputs.Params) != len(raw.Outputs.Params) {
		t.Fatalf("params: got %d keys, fixture has %d", len(resp.Outputs.Params), len(raw.Outputs.Params))
	}
	for key, value := range raw.Outputs.Params {
		if resp.Outputs.Params[key] != value {
			t.Fatalf("params[%q]: got %q, want %q", key, resp.Outputs.Params[key], value)
		}
	}
	if len(resp.Outputs.Extras) != 1 || resp.Outputs.Extras["registryLoginServer"] != "crvexdemosand.azurecr.io" {
		t.Fatalf("extras: got %v", resp.Outputs.Extras)
	}
}

func TestPortalClient_CloudConnection_NotUsable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		fixture, status, reason, hint string
	}{
		{"connection_absent.json", "absent", "no_connection", "vex bootstrap sand"},
		{"connection_revoked.json", "revoked", "revoked", "vex bootstrap sand"},
		{"connection_failed.json", "failed", "verify_failed", "vex bootstrap sand --reconnect"},
		{"connection_pending.json", "pending", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			t.Parallel()
			client, _ := newCloudServer(t, http.StatusOK, nil, cloudFixture(t, tt.fixture))

			resp, err := client.CloudConnection(context.Background(), "proj-1", "sand")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.Status != tt.status || resp.Reason != tt.reason || resp.Hint != tt.hint {
				t.Fatalf("got %+v", resp)
			}
			if resp.Outputs != nil {
				t.Fatalf("outputs must be nil unless active: %+v", resp.Outputs)
			}
		})
	}
}

func TestPortalClient_CloudConnection_ProtocolErrors(t *testing.T) {
	t.Parallel()

	bodies := map[string]string{
		"active without outputs":     `{"status":"active","connection_id":"c1","outputs":null}`,
		"active without contract id": `{"status":"active","connection_id":"c1","outputs":{"contract_id":"","params":{}}}`,
		"no status":                  `{}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client, _ := newCloudServer(t, http.StatusOK, nil, []byte(body))

			resp, err := client.CloudConnection(context.Background(), "proj-1", "sand")
			if err == nil {
				t.Fatalf("want a protocol error, got %+v", resp)
			}
			if resp.Status != "" || resp.Outputs != nil {
				t.Fatalf("a rejected response must not leak out: %+v", resp)
			}
		})
	}
}

func TestPortalClient_Cloud_RequiredArguments(t *testing.T) {
	t.Parallel()
	client, seen := newCloudServer(t, http.StatusOK, nil, cloudFixture(t, "connection_active.json"))

	if _, err := client.CloudConnection(context.Background(), "", "sand"); err == nil {
		t.Fatal("empty project id must be rejected")
	}
	if _, err := client.CloudConnection(context.Background(), "proj-1", ""); err == nil {
		t.Fatal("empty environment must be rejected")
	}
	if _, err := client.CloudToken(context.Background(), CloudTokenRequest{Environment: "sand"}); err == nil {
		t.Fatal("empty project id must be rejected")
	}
	if _, err := client.CloudToken(context.Background(), CloudTokenRequest{ProjectID: "proj-1"}); err == nil {
		t.Fatal("empty environment must be rejected")
	}
	if seen.method != "" {
		t.Fatalf("invalid arguments must not reach the network: %+v", seen)
	}
}

func TestPortalClient_CloudToken_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		req      CloudTokenRequest
		wantBody string
	}{
		{
			name:     "only the required fields",
			req:      CloudTokenRequest{ProjectID: "proj-1", Environment: "sand"},
			wantBody: `{"project_id":"proj-1","environment":"sand"}`,
		},
		{
			name: "every field",
			req: CloudTokenRequest{
				ProjectID: "proj-1", Environment: "sand", AttemptID: "i1", Commit: "abc1234", ExecutionID: "exec-1",
			},
			wantBody: `{"project_id":"proj-1","environment":"sand","attempt_id":"i1","commit":"abc1234","execution_id":"exec-1"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client, seen := newCloudServer(t, http.StatusOK, nil, cloudFixture(t, "token_issued.json"))

			resp, err := client.CloudToken(context.Background(), tt.req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if seen.method != http.MethodPost || seen.path != "/functions/v1/cli-cloud-token" || seen.rawQuery != "" {
				t.Fatalf("request: got %s %s?%s", seen.method, seen.path, seen.rawQuery)
			}
			if seen.auth != "Bearer test-token" || seen.contentType != "application/json" {
				t.Fatalf("headers: auth %q, content-type %q", seen.auth, seen.contentType)
			}
			if string(seen.body) != tt.wantBody {
				t.Fatalf("body: got %s, want %s", seen.body, tt.wantBody)
			}
			if !strings.HasPrefix(resp.Token, "eyJ") {
				t.Fatalf("token not decoded")
			}
			if want := time.Date(2026, 7, 10, 15, 10, 0, 0, time.UTC); !resp.ExpiresAt.Equal(want) {
				t.Fatalf("expires_at: got %v, want %v", resp.ExpiresAt, want)
			}
			if want := time.Date(2026, 7, 10, 15, 5, 0, 0, time.UTC); !resp.RefreshAfter.Equal(want) {
				t.Fatalf("refresh_after: got %v, want %v", resp.RefreshAfter, want)
			}
		})
	}
}

func TestPortalClient_CloudToken_InvalidToken(t *testing.T) {
	t.Parallel()

	bodies := map[string]string{
		"empty token":       `{"token":"","expires_at":"2026-07-10T15:10:00Z","refresh_after":"2026-07-10T15:05:00Z"}`,
		"already expired":   `{"token":"eyJhbGciOiJSUzI1NiJ9.e30.c2ln","expires_at":"2026-07-10T14:59:59Z","refresh_after":"2026-07-10T14:55:00Z"}`,
		"no expiry":         `{"token":"eyJhbGciOiJSUzI1NiJ9.e30.c2ln","refresh_after":"2026-07-10T15:05:00Z"}`,
		"no refresh_after":  `{"token":"eyJhbGciOiJSUzI1NiJ9.e30.c2ln","expires_at":"2026-07-10T15:10:00Z"}`,
		"malformed expires": `{"token":"eyJhbGciOiJSUzI1NiJ9.e30.c2ln","expires_at":"mañana","refresh_after":"2026-07-10T15:05:00Z"}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client, _ := newCloudServer(t, http.StatusOK, nil, []byte(body))

			resp, err := client.CloudToken(context.Background(), CloudTokenRequest{ProjectID: "proj-1", Environment: "sand"})
			if err == nil {
				t.Fatal("want an error")
			}
			if resp.Token != "" {
				t.Fatal("a rejected token must not be returned")
			}
			if strings.Contains(err.Error(), "eyJ") {
				t.Fatalf("the error leaks the token: %v", err)
			}
		})
	}
}

func TestPortalClient_CloudToken_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     int
		fixture    string
		header     http.Header
		want       error
		wantNot    []error
		retryAfter int
	}{
		{name: "401", status: http.StatusUnauthorized, fixture: "error_invalid_token.json", want: ErrUnauthorized},
		{name: "403", status: http.StatusForbidden, fixture: "error_forbidden.json", want: ErrForbidden},
		{
			name: "409 connection_not_active", status: http.StatusConflict, fixture: "error_connection_not_active.json",
			want: ErrConnectionNotActive, wantNot: []error{ErrConflict},
		},
		{
			name: "429 rate_limited", status: http.StatusTooManyRequests, fixture: "error_rate_limited.json",
			header: http.Header{"Retry-After": []string{"3600"}},
			want:   ErrRateLimited, wantNot: []error{ErrUserConcurrencyLimit}, retryAfter: 3600,
		},
		{
			name: "502 signing_unavailable", status: http.StatusBadGateway, fixture: "error_signing_unavailable.json",
			want: ErrSigningUnavailable, wantNot: []error{ErrFlyAPIFailure},
		},
		{
			name: "503 audit_unavailable", status: http.StatusServiceUnavailable, fixture: "error_audit_unavailable.json",
			want: ErrAuditUnavailable, wantNot: []error{ErrGlobalCapacityReached},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client, _ := newCloudServer(t, tt.status, tt.header, cloudFixture(t, tt.fixture))

			_, err := client.CloudToken(context.Background(), CloudTokenRequest{ProjectID: "proj-1", Environment: "sand"})

			if !errors.Is(err, tt.want) {
				t.Fatalf("want %v, got %v", tt.want, err)
			}
			for _, other := range tt.wantNot {
				if errors.Is(err, other) {
					t.Fatalf("must not be %v: the response code wins over the status", other)
				}
			}
			var httpErr *HTTPError
			if !errors.As(err, &httpErr) || httpErr.StatusCode != tt.status {
				t.Fatalf("want *HTTPError with status %d, got %v", tt.status, err)
			}
			if httpErr.RetryAfter != tt.retryAfter {
				t.Fatalf("RetryAfter: got %d, want %d", httpErr.RetryAfter, tt.retryAfter)
			}
		})
	}
}

// The portal must never echo a token, but if a proxy or a bug does, the
// error that reaches the terminal must not carry it.
func TestPortalClient_Cloud_ErrorsNeverLeakAToken(t *testing.T) {
	t.Parallel()

	const jwt = "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJzZWNyZXRvIn0.ZmlybWEtc2VjcmV0YQ"
	bodies := map[string]string{
		"json message": `{"error":"internal","message":"no se pudo guardar ` + jwt + ` en la auditoría"}`,
		"json code":    `{"error":"` + jwt + `"}`,
		"raw body":     "upstream said: " + jwt,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client, _ := newCloudServer(t, http.StatusInternalServerError, nil, []byte(body))

			_, tokenErr := client.CloudToken(context.Background(), CloudTokenRequest{ProjectID: "proj-1", Environment: "sand"})
			_, connErr := client.CloudConnection(context.Background(), "proj-1", "sand")

			for _, err := range []error{tokenErr, connErr} {
				if err == nil {
					t.Fatal("want an error")
				}
				shown := fmt.Sprintf("%v %+v %#v", err, err, errors.Unwrap(err))
				for _, part := range []string{"eyJ", "eyJzdWIiOiJzZWNyZXRvIn0", "ZmlybWEtc2VjcmV0YQ"} {
					if strings.Contains(shown, part) {
						t.Fatalf("the error leaks the token (%q): %s", part, shown)
					}
				}
			}
		})
	}
}

func TestCloudTokenResponse_NeverPrintsTheToken(t *testing.T) {
	t.Parallel()

	resp := CloudTokenResponse{Token: "eyJhbGciOiJSUzI1NiJ9.c2VjcmV0bw.c2ln", ExpiresAt: fixtureNow, RefreshAfter: fixtureNow}
	shown := fmt.Sprintf("%v %+v %#v %s %v", resp, resp, resp, resp, &resp)
	if strings.Contains(shown, "eyJ") || strings.Contains(shown, "c2VjcmV0bw") {
		t.Fatalf("the token is printed: %s", shown)
	}
}

func TestPortalClient_Cloud_WithoutSessionNeverTouchesTheNetwork(t *testing.T) {
	t.Parallel()

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer srv.Close()
	emptyStore := portalauth.NewFileTokenStoreAt(filepath.Join(t.TempDir(), "credentials.json"))
	client := NewPortalClient(srv.URL, emptyStore, srv.Client())

	_, connErr := client.CloudConnection(context.Background(), "proj-1", "sand")
	_, tokenErr := client.CloudToken(context.Background(), CloudTokenRequest{ProjectID: "proj-1", Environment: "sand"})

	if !errors.Is(connErr, portalauth.ErrTokenNotFound) || !errors.Is(tokenErr, portalauth.ErrTokenNotFound) {
		t.Fatalf("want ErrTokenNotFound, got %v / %v", connErr, tokenErr)
	}
	if hits != 0 {
		t.Fatalf("the network was reached %d times", hits)
	}
}

func TestPortalClient_CloudToken_DoesNotRetry(t *testing.T) {
	t.Parallel()

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"signing_unavailable"}`))
	}))
	defer srv.Close()
	client := NewPortalClient(srv.URL, newTestStore(t), srv.Client())

	if _, err := client.CloudToken(context.Background(), CloudTokenRequest{ProjectID: "proj-1", Environment: "sand"}); err == nil {
		t.Fatal("want an error")
	}
	if hits != 1 {
		t.Fatalf("every call counts against the rate limit: got %d requests, want 1", hits)
	}
}
