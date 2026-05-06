package portalclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/jairoprogramador/vex/internal/infrastructure/portalauth"
)

// newTestStore returns a token store backed by a temp directory and
// pre-populated with a valid bearer token. Tests use it whenever they need
// to hit the client surface.
func newTestStore(t *testing.T) *portalauth.FileTokenStore {
	t.Helper()
	store := portalauth.NewFileTokenStoreAt(filepath.Join(t.TempDir(), "credentials.json"))
	token := portalauth.Token{
		AccessToken: "test-token",
		TokenType:   "Bearer",
		ObtainedAt:  time.Now().UTC(),
		ExpiresAt:   time.Now().Add(1 * time.Hour).UTC(),
	}
	if err := store.Save(token); err != nil {
		t.Fatalf("seed token store: %v", err)
	}
	return store
}

func TestPortalClient_CreateOrGetProject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		body       string
		header     http.Header
		assert     func(t *testing.T, resp CreateOrGetProjectResponse, err error)
	}{
		{
			name:       "success without authoritative",
			statusCode: http.StatusOK,
			body: `{
				"project_id": "proj-1",
				"pipeline_id": "pipe-1",
				"created": true,
				"needs_sync": true
			}`,
			assert: func(t *testing.T, resp CreateOrGetProjectResponse, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if resp.ProjectID != "proj-1" {
					t.Fatalf("project_id: got %q", resp.ProjectID)
				}
				if !resp.Created || !resp.NeedsSync {
					t.Fatalf("created/needs_sync: got %+v", resp)
				}
				if resp.Authoritative != nil {
					t.Fatalf("authoritative: got %+v, want nil", resp.Authoritative)
				}
			},
		},
		{
			name:       "success with authoritative",
			statusCode: http.StatusOK,
			body: `{
				"project_id": "proj-1",
				"pipeline_id": "pipe-1",
				"created": false,
				"needs_sync": false,
				"authoritative": {
					"project": {"repo_url": "https://github.com/org/repo", "repo_ref": "main"},
					"pipeline": {
						"url": "https://github.com/org/pipe", "ref": "main",
						"runtime_image": "ghcr.io/vex/runtime", "runtime_tag": "v2"
					}
				}
			}`,
			assert: func(t *testing.T, resp CreateOrGetProjectResponse, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if resp.Authoritative == nil {
					t.Fatalf("authoritative was nil")
				}
				if resp.Authoritative.Pipeline.URL != "https://github.com/org/pipe" {
					t.Fatalf("authoritative pipeline url: got %q", resp.Authoritative.Pipeline.URL)
				}
				if resp.Authoritative.Pipeline.RuntimeTag != "v2" {
					t.Fatalf("authoritative runtime tag: got %q", resp.Authoritative.Pipeline.RuntimeTag)
				}
				if resp.Authoritative.Project.RepoRef != "main" {
					t.Fatalf("authoritative repo ref: got %q", resp.Authoritative.Project.RepoRef)
				}
			},
		},
		{
			name:       "unauthorized",
			statusCode: http.StatusUnauthorized,
			body:       `{"error":"unauthorized"}`,
			assert: func(t *testing.T, _ CreateOrGetProjectResponse, err error) {
				if !errors.Is(err, ErrUnauthorized) {
					t.Fatalf("want ErrUnauthorized, got %v", err)
				}
			},
		},
		{
			name:       "forbidden",
			statusCode: http.StatusForbidden,
			body:       `{"error":"forbidden"}`,
			assert: func(t *testing.T, _ CreateOrGetProjectResponse, err error) {
				if !errors.Is(err, ErrForbidden) {
					t.Fatalf("want ErrForbidden, got %v", err)
				}
			},
		},
		{
			name:       "fly failure",
			statusCode: http.StatusBadGateway,
			body:       `{"error":"fly_api_failure","message":"machines unreachable"}`,
			assert: func(t *testing.T, _ CreateOrGetProjectResponse, err error) {
				if !errors.Is(err, ErrFlyAPIFailure) {
					t.Fatalf("want ErrFlyAPIFailure, got %v", err)
				}
				var httpErr *HTTPError
				if !errors.As(err, &httpErr) {
					t.Fatalf("want HTTPError, got %T", err)
				}
				if httpErr.Code != "fly_api_failure" {
					t.Fatalf("HTTPError.Code: got %q", httpErr.Code)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/functions/v1/create-or-get-project" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				if r.Method != http.MethodPost {
					t.Errorf("unexpected method: %s", r.Method)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
					t.Errorf("authorization header: got %q", got)
				}
				if got := r.Header.Get("Content-Type"); got != "application/json" {
					t.Errorf("content-type header: got %q", got)
				}
				body, _ := io.ReadAll(r.Body)
				var decoded CreateOrGetProjectRequest
				if err := json.Unmarshal(body, &decoded); err != nil {
					t.Errorf("decode body: %v", err)
				}
				if decoded.Project.ID == "" {
					t.Errorf("project.id: empty in body %s", body)
				}
				w.Header().Set("Content-Type", "application/json")
				for k, vs := range tt.header {
					for _, v := range vs {
						w.Header().Add(k, v)
					}
				}
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			client := NewPortalClient(srv.URL, newTestStore(t), srv.Client())
			resp, err := client.CreateOrGetProject(context.Background(), CreateOrGetProjectRequest{
				Project: ProjectPayload{
					ID:           "proj-1",
					Name:         "demo",
					Team:         "shikigami",
					Organization: "vex",
					RepoURL:      "https://github.com/org/repo",
					RepoRef:      "main",
				},
				Pipeline: PipelinePayload{
					URL:          "https://github.com/org/pipe",
					Ref:          "main",
					RuntimeImage: "ghcr.io/vex/runtime",
					RuntimeTag:   "v2",
				},
			})
			tt.assert(t, resp, err)
		})
	}
}

func TestPortalClient_TriggerDeploy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		body       string
		header     http.Header
		assert     func(t *testing.T, resp TriggerDeployResponse, err error)
	}{
		{
			name:       "success",
			statusCode: http.StatusOK,
			body: `{
				"execution_id": "exec-42",
				"status": "pending",
				"follow_url": "https://portal/follow",
				"portal_url": "https://portal/p/x/deploy"
			}`,
			assert: func(t *testing.T, resp TriggerDeployResponse, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if resp.ExecutionID != "exec-42" {
					t.Fatalf("execution_id: got %q", resp.ExecutionID)
				}
				if resp.Status != "pending" {
					t.Fatalf("status: got %q", resp.Status)
				}
			},
		},
		{
			name:       "user concurrency limit (429 with code)",
			statusCode: http.StatusTooManyRequests,
			body:       `{"error":"concurrent_execution_limit","limit":3}`,
			assert: func(t *testing.T, _ TriggerDeployResponse, err error) {
				if !errors.Is(err, ErrUserConcurrencyLimit) {
					t.Fatalf("want ErrUserConcurrencyLimit, got %v", err)
				}
			},
		},
		{
			name:       "global capacity reached (503 with retry-after)",
			statusCode: http.StatusServiceUnavailable,
			body:       `{"error":"global_capacity_reached"}`,
			header:     http.Header{"Retry-After": []string{"30"}},
			assert: func(t *testing.T, _ TriggerDeployResponse, err error) {
				if !errors.Is(err, ErrGlobalCapacityReached) {
					t.Fatalf("want ErrGlobalCapacityReached, got %v", err)
				}
				var httpErr *HTTPError
				if !errors.As(err, &httpErr) {
					t.Fatalf("want HTTPError, got %T", err)
				}
				if httpErr.RetryAfter != 30 {
					t.Fatalf("RetryAfter: got %d, want 30", httpErr.RetryAfter)
				}
			},
		},
		{
			name:       "unauthorized",
			statusCode: http.StatusUnauthorized,
			body:       `{"error":"invalid_token"}`,
			assert: func(t *testing.T, _ TriggerDeployResponse, err error) {
				if !errors.Is(err, ErrUnauthorized) {
					t.Fatalf("want ErrUnauthorized, got %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/functions/v1/trigger-deploy" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				for k, vs := range tt.header {
					for _, v := range vs {
						w.Header().Add(k, v)
					}
				}
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			client := NewPortalClient(srv.URL, newTestStore(t), srv.Client())
			resp, err := client.TriggerDeploy(context.Background(), TriggerDeployRequest{
				PipelineID:  "pipe-1",
				Environment: "prod",
				Step:        "deploy",
			})
			tt.assert(t, resp, err)
		})
	}
}

func TestPortalClient_CancelExecution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		execID     string
		statusCode int
		body       string
		wantErrIs  error
	}{
		{
			name:       "success",
			execID:     "exec-42",
			statusCode: http.StatusOK,
			body:       `{"status":"canceled"}`,
		},
		{
			name:      "empty id rejected client-side",
			execID:    "",
			wantErrIs: nil, // we just check err != nil below
		},
		{
			name:       "not found",
			execID:     "exec-missing",
			statusCode: http.StatusNotFound,
			body:       `{"error":"not_found"}`,
			wantErrIs:  ErrNotFound,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var srv *httptest.Server
			if tt.statusCode != 0 {
				srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/functions/v1/cancel-execution" {
						t.Errorf("unexpected path: %s", r.URL.Path)
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(tt.statusCode)
					_, _ = w.Write([]byte(tt.body))
				}))
				defer srv.Close()
			}

			baseURL := ""
			var hc *http.Client
			if srv != nil {
				baseURL = srv.URL
				hc = srv.Client()
			}
			client := NewPortalClient(baseURL, newTestStore(t), hc)
			err := client.CancelExecution(context.Background(), tt.execID)
			if tt.execID == "" {
				if err == nil {
					t.Fatalf("expected error for empty execution id")
				}
				return
			}
			if tt.wantErrIs != nil {
				if !errors.Is(err, tt.wantErrIs) {
					t.Fatalf("want errors.Is(%v), got %v", tt.wantErrIs, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestPortalClient_SyncPipeline(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/functions/v1/sync-pipeline" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			body, _ := io.ReadAll(r.Body)
			var decoded SyncPipelineRequest
			if err := json.Unmarshal(body, &decoded); err != nil {
				t.Errorf("decode body: %v", err)
			}
			if decoded.PipelineID != "pipe-1" {
				t.Errorf("pipeline_id: got %q", decoded.PipelineID)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}))
		defer srv.Close()

		client := NewPortalClient(srv.URL, newTestStore(t), srv.Client())
		if err := client.SyncPipeline(context.Background(), "pipe-1"); err != nil {
			t.Fatalf("sync pipeline: %v", err)
		}
	})

	t.Run("rejects empty id", func(t *testing.T) {
		t.Parallel()
		client := NewPortalClient("http://unused", newTestStore(t), http.DefaultClient)
		err := client.SyncPipeline(context.Background(), "")
		if err == nil {
			t.Fatalf("expected error for empty pipeline id")
		}
	})
}

func TestPortalClient_NoToken(t *testing.T) {
	t.Parallel()

	store := portalauth.NewFileTokenStoreAt(filepath.Join(t.TempDir(), "credentials.json"))
	client := NewPortalClient("http://unused", store, http.DefaultClient)
	_, err := client.CreateOrGetProject(context.Background(), CreateOrGetProjectRequest{})
	if !errors.Is(err, portalauth.ErrTokenNotFound) {
		t.Fatalf("want ErrTokenNotFound, got %v", err)
	}
}
