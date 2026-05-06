package application

import (
	"errors"
	"fmt"
	"testing"

	comVos "github.com/jairoprogramador/vex/internal/domain/common/vos"
	"github.com/jairoprogramador/vex/internal/domain/project/aggregates"
	proVos "github.com/jairoprogramador/vex/internal/domain/project/vos"
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
	image, err := comVos.NewImage("local/runtime", "v1")
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
			wantSubst: "vex auth login",
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
