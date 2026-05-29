package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	comVos "github.com/jairoprogramador/vex/internal/domain/common/vos"
	"github.com/jairoprogramador/vex/internal/domain/project/aggregates"
	proVos "github.com/jairoprogramador/vex/internal/domain/project/vos"
	"github.com/jairoprogramador/vex/internal/infrastructure/portalauth"
	"github.com/jairoprogramador/vex/internal/infrastructure/portalclient"
)

// stubProjectRepo records Save calls and returns the configured project on Load.
type stubProjectRepo struct {
	loaded     *aggregates.Project
	saveCalls  int
	saveErr    error
	saved      *aggregates.Project
	existsBool bool
}

func (s *stubProjectRepo) Save(p *aggregates.Project) error {
	s.saveCalls++
	s.saved = p
	return s.saveErr
}

func (s *stubProjectRepo) Exists() (bool, error) {
	return s.existsBool, nil
}

func (s *stubProjectRepo) Load() (*aggregates.Project, error) {
	return s.loaded, nil
}

// newSeedProject builds a Project with known values that tests then mutate
// against the portal's authoritative payload.
func newSeedProject(t *testing.T) *aggregates.Project {
	t.Helper()

	id, err := proVos.NewProjectID("seed-id")
	if err != nil {
		t.Fatalf("project id: %v", err)
	}
	data, err := proVos.NewProjectData("demo", "vex", "shikigami", "desc",
		"https://github.com/local/repo", "main")
	if err != nil {
		t.Fatalf("project data: %v", err)
	}
	pipeline, err := comVos.NewPipeline("https://github.com/local/pipe", "main")
	if err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	image, err := comVos.NewImage("local/runtime:v1")
	if err != nil {
		t.Fatalf("image: %v", err)
	}
	runtime := proVos.NewRuntime(proVos.WithImage(image))
	project, err := aggregates.NewProject(id, data, pipeline, runtime)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	return project
}

func TestApplyAuthoritative(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		auth    *portalclient.AuthoritativeData
		wantSet bool
		check   func(t *testing.T, p *aggregates.Project)
	}{
		{
			name: "no changes when fields match",
			auth: &portalclient.AuthoritativeData{
				Project: portalclient.AuthoritativeProject{
					RepoURL: "https://github.com/local/repo",
					RepoRef: "main",
				},
				Pipeline: portalclient.AuthoritativePipeline{
					URL:          "https://github.com/local/pipe",
					Ref:          "main",
					RuntimeImage: "local/runtime",
					RuntimeTag:   "v1",
				},
			},
			wantSet: false,
		},
		{
			name: "pipeline url drifts",
			auth: &portalclient.AuthoritativeData{
				Project: portalclient.AuthoritativeProject{
					RepoURL: "https://github.com/local/repo",
					RepoRef: "main",
				},
				Pipeline: portalclient.AuthoritativePipeline{
					URL:          "https://github.com/portal/pipe",
					Ref:          "main",
					RuntimeImage: "local/runtime",
					RuntimeTag:   "v1",
				},
			},
			wantSet: true,
			check: func(t *testing.T, p *aggregates.Project) {
				if got := p.Pipeline().URL(); got != "https://github.com/portal/pipe" {
					t.Fatalf("pipeline url: got %q", got)
				}
			},
		},
		{
			name: "runtime image and tag drift",
			auth: &portalclient.AuthoritativeData{
				Project: portalclient.AuthoritativeProject{
					RepoURL: "https://github.com/local/repo",
					RepoRef: "main",
				},
				Pipeline: portalclient.AuthoritativePipeline{
					URL:          "https://github.com/local/pipe",
					Ref:          "main",
					RuntimeImage: "ghcr.io/vex/runtime",
					RuntimeTag:   "v2",
				},
			},
			wantSet: true,
			check: func(t *testing.T, p *aggregates.Project) {
				if got := p.Runtime().Image().Image(); got != "ghcr.io/vex/runtime" {
					t.Fatalf("runtime image: got %q", got)
				}
				if got := p.Runtime().Image().Tag(); got != "v2" {
					t.Fatalf("runtime tag: got %q", got)
				}
			},
		},
		{
			name: "project repo url drifts",
			auth: &portalclient.AuthoritativeData{
				Project: portalclient.AuthoritativeProject{
					RepoURL: "https://github.com/portal/repo",
					RepoRef: "main",
				},
				Pipeline: portalclient.AuthoritativePipeline{
					URL:          "https://github.com/local/pipe",
					Ref:          "main",
					RuntimeImage: "local/runtime",
					RuntimeTag:   "v1",
				},
			},
			wantSet: true,
			check: func(t *testing.T, p *aggregates.Project) {
				if got := p.Data().URL(); got != "https://github.com/portal/repo" {
					t.Fatalf("project repo url: got %q", got)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &stubProjectRepo{}
			svc := &RemoteExecutorService{projectRepository: repo}
			project := newSeedProject(t)

			changed, err := svc.applyAuthoritative(project, tt.auth)
			if err != nil {
				t.Fatalf("applyAuthoritative: %v", err)
			}
			if changed != tt.wantSet {
				t.Fatalf("changed: got %v, want %v", changed, tt.wantSet)
			}
			if tt.wantSet {
				if repo.saveCalls != 1 {
					t.Fatalf("saveCalls: got %d, want 1", repo.saveCalls)
				}
				if tt.check != nil {
					tt.check(t, project)
				}
			} else if repo.saveCalls != 0 {
				t.Fatalf("saveCalls on no-op: got %d", repo.saveCalls)
			}
		})
	}
}

func TestTranslateError(t *testing.T) {
	t.Parallel()

	svc := &RemoteExecutorService{}
	cases := []struct {
		name      string
		in        error
		wantSubst string
	}{
		{
			name:      "user concurrency",
			in:        fmt.Errorf("wrap: %w", portalclient.ErrUserConcurrencyLimit),
			wantSubst: "too many running executions",
		},
		{
			name:      "global capacity without retry",
			in:        fmt.Errorf("wrap: %w", portalclient.ErrGlobalCapacityReached),
			wantSubst: "Retry shortly",
		},
		{
			name:      "unauthorized",
			in:        fmt.Errorf("wrap: %w", portalclient.ErrUnauthorized),
			wantSubst: "vex login",
		},
		{
			name:      "forbidden",
			in:        fmt.Errorf("wrap: %w", portalclient.ErrForbidden),
			wantSubst: "membership required",
		},
		{
			name:      "fly failure",
			in:        fmt.Errorf("wrap: %w", portalclient.ErrFlyAPIFailure),
			wantSubst: "runner machine",
		},
		{
			name:      "passthrough unknown",
			in:        errors.New("kaboom"),
			wantSubst: "kaboom",
		},
	}

	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out := svc.translateError(tt.in)
			if out == nil {
				t.Fatalf("expected non-nil error")
			}
			if got := out.Error(); !contains(got, tt.wantSubst) {
				t.Fatalf("translateError: got %q, want substring %q", got, tt.wantSubst)
			}
		})
	}
}

func TestBuildCreateOrGetRequest(t *testing.T) {
	t.Parallel()

	project := newSeedProject(t)
	req := buildCreateOrGetRequest(project)

	if req.Project.ID != project.ID().String() {
		t.Fatalf("project id: got %q", req.Project.ID)
	}
	if req.Pipeline.URL != "https://github.com/local/pipe" {
		t.Fatalf("pipeline url: got %q", req.Pipeline.URL)
	}
	if req.Pipeline.RuntimeImage != "local/runtime" || req.Pipeline.RuntimeTag != "v1" {
		t.Fatalf("runtime image/tag: got %s:%s", req.Pipeline.RuntimeImage, req.Pipeline.RuntimeTag)
	}
}

// triggerDeployServer wires an httptest.Server that lets each test choose
// the response of the trigger-deploy endpoint per call. Only this endpoint
// is implemented; all other paths return 404. The handler counts calls so
// tests can assert on retry behavior.
type triggerDeployServer struct {
	server     *httptest.Server
	calls      atomic.Int32
	respondFns []http.HandlerFunc
}

func newTriggerDeployServer(t *testing.T, fns ...http.HandlerFunc) *triggerDeployServer {
	t.Helper()
	s := &triggerDeployServer{respondFns: fns}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/functions/v1/cli-trigger-deploy" {
			http.NotFound(w, r)
			return
		}
		idx := int(s.calls.Add(1)) - 1
		if idx >= len(s.respondFns) {
			t.Errorf("trigger-deploy hit %d times, only %d responses configured", idx+1, len(s.respondFns))
			http.Error(w, "no response configured", http.StatusInternalServerError)
			return
		}
		s.respondFns[idx](w, r)
	}))
	t.Cleanup(s.server.Close)
	return s
}

// newSeededTokenStore mirrors portalclient.newTestStore but lives here so
// the application package tests don't import unexported test helpers.
func newSeededTokenStore(t *testing.T) *portalauth.FileTokenStore {
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

func writeTriggerDeploy503(t *testing.T, retryAfter int) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if retryAfter > 0 {
			w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":   "global_capacity_reached",
			"message": "server at capacity",
		})
	}
}

func writeTriggerDeploy200(t *testing.T, executionID string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"execution_id": executionID,
			"portal_url":   "https://example.test/exec/" + executionID,
		})
	}
}

func TestTriggerDeployWithCapacityRetry_FollowRetriesOnce(t *testing.T) {
	t.Parallel()

	srv := newTriggerDeployServer(t,
		// 1st call: 503 with a tiny Retry-After so the test stays fast.
		writeTriggerDeploy503(t, 1),
		// 2nd call: success.
		writeTriggerDeploy200(t, "exec-success"),
	)
	store := newSeededTokenStore(t)
	client := portalclient.NewPortalClient(srv.server.URL, store, srv.server.Client())

	svc := &RemoteExecutorService{
		portalClient: client,
		follow:       true,
		stdout:       &discardWriter{},
		stderr:       &discardWriter{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := svc.triggerDeployWithCapacityRetry(ctx, portalclient.TriggerDeployRequest{
		PipelineID:  "pipe-1",
		Environment: "prod",
		Step:        "deploy",
	})
	if err != nil {
		t.Fatalf("triggerDeployWithCapacityRetry: %v", err)
	}
	if resp.ExecutionID != "exec-success" {
		t.Fatalf("execution id: got %q want %q", resp.ExecutionID, "exec-success")
	}
	if got := srv.calls.Load(); got != 2 {
		t.Fatalf("trigger-deploy calls: got %d, want 2", got)
	}
}

func TestTriggerDeployWithCapacityRetry_NoFollowFailsImmediately(t *testing.T) {
	t.Parallel()

	srv := newTriggerDeployServer(t,
		writeTriggerDeploy503(t, 30),
	)
	store := newSeededTokenStore(t)
	client := portalclient.NewPortalClient(srv.server.URL, store, srv.server.Client())

	svc := &RemoteExecutorService{
		portalClient: client,
		follow:       false,
		stdout:       &discardWriter{},
		stderr:       &discardWriter{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := svc.triggerDeployWithCapacityRetry(ctx, portalclient.TriggerDeployRequest{
		PipelineID:  "pipe-1",
		Environment: "prod",
		Step:        "deploy",
	})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, portalclient.ErrGlobalCapacityReached) {
		t.Fatalf("expected ErrGlobalCapacityReached, got %v", err)
	}
	if got := srv.calls.Load(); got != 1 {
		t.Fatalf("trigger-deploy calls: got %d, want 1 (no retry on --no-follow)", got)
	}
}

func TestTriggerDeployWithCapacityRetry_RetryAlsoFails(t *testing.T) {
	t.Parallel()

	srv := newTriggerDeployServer(t,
		writeTriggerDeploy503(t, 1),
		writeTriggerDeploy503(t, 1),
	)
	store := newSeededTokenStore(t)
	client := portalclient.NewPortalClient(srv.server.URL, store, srv.server.Client())

	svc := &RemoteExecutorService{
		portalClient: client,
		follow:       true,
		stdout:       &discardWriter{},
		stderr:       &discardWriter{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := svc.triggerDeployWithCapacityRetry(ctx, portalclient.TriggerDeployRequest{
		PipelineID:  "pipe-1",
		Environment: "prod",
		Step:        "deploy",
	})
	if err == nil {
		t.Fatalf("expected error after second 503, got nil")
	}
	if !errors.Is(err, portalclient.ErrGlobalCapacityReached) {
		t.Fatalf("expected ErrGlobalCapacityReached, got %v", err)
	}
	if got := srv.calls.Load(); got != 2 {
		t.Fatalf("trigger-deploy calls: got %d, want 2 (retry + final failure)", got)
	}
}

func TestTriggerDeployWithCapacityRetry_OtherErrorsNotRetried(t *testing.T) {
	t.Parallel()

	srv := newTriggerDeployServer(t,
		// 429 user concurrency → must not retry even with --follow.
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":   "concurrent_execution_limit",
				"message": "too many running",
			})
		},
	)
	store := newSeededTokenStore(t)
	client := portalclient.NewPortalClient(srv.server.URL, store, srv.server.Client())

	svc := &RemoteExecutorService{
		portalClient: client,
		follow:       true,
		stdout:       &discardWriter{},
		stderr:       &discardWriter{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := svc.triggerDeployWithCapacityRetry(ctx, portalclient.TriggerDeployRequest{
		PipelineID:  "pipe-1",
		Environment: "prod",
		Step:        "deploy",
	})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, portalclient.ErrUserConcurrencyLimit) {
		t.Fatalf("expected ErrUserConcurrencyLimit, got %v", err)
	}
	if got := srv.calls.Load(); got != 1 {
		t.Fatalf("trigger-deploy calls: got %d, want 1 (429 must not be retried)", got)
	}
}

// discardWriter is an io.Writer that throws away its input. Used to silence
// progress messages during tests without depending on io.Discard's package
// path conventions.
type discardWriter struct{}

func (*discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// contains is a tiny strings.Contains alias kept local so the test file
// stays import-light; brings no extra dependencies into the package.
func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
