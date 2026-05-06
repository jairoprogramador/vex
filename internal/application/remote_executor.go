package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	comVos "github.com/jairoprogramador/vex/internal/domain/common/vos"
	"github.com/jairoprogramador/vex/internal/domain/project/aggregates"
	proPor "github.com/jairoprogramador/vex/internal/domain/project/ports"
	proVos "github.com/jairoprogramador/vex/internal/domain/project/vos"
	"github.com/jairoprogramador/vex/internal/infrastructure/portalauth"
	"github.com/jairoprogramador/vex/internal/infrastructure/portalclient"
)

// RemoteExecutorService orchestrates a deploy that runs on the portal-side
// infrastructure (Fly Machines) instead of the local Docker daemon. It is
// the entry point of the `vex <step> [env] --remote` flow.
//
// The service is intentionally CLI-aware: it prints user-facing messages
// directly to its configured writers (defaulting to os.Stdout / os.Stderr).
// Only the Run method is part of the Runner contract; helpers are private
// so wiring stays focused.
type RemoteExecutorService struct {
	projectRepository proPor.ProjectRepository
	portalClient      *portalclient.PortalClient
	deviceFlow        *portalauth.DeviceFlowClient
	tokenStore        *portalauth.FileTokenStore
	follow            bool

	// Surfaces decoupled from the package globals so tests (or future
	// non-CLI callers) can capture them. Default to os.Stdout / os.Stderr
	// when nil.
	stdout io.Writer
	stderr io.Writer
	// now is overridable by tests. It backs the timestamp on the persisted
	// token after a re-login.
	now func() time.Time
}

// RemoteExecutorOption customizes a RemoteExecutorService at construction
// time. Production wiring uses defaults; tests inject writers / clocks.
type RemoteExecutorOption func(*RemoteExecutorService)

// WithStdout overrides the writer used for user-facing progress messages.
func WithStdout(w io.Writer) RemoteExecutorOption {
	return func(s *RemoteExecutorService) { s.stdout = w }
}

// WithStderr overrides the writer used for warnings and recoverable errors.
func WithStderr(w io.Writer) RemoteExecutorOption {
	return func(s *RemoteExecutorService) { s.stderr = w }
}

// WithClock overrides the time source used to stamp persisted tokens.
func WithClock(now func() time.Time) RemoteExecutorOption {
	return func(s *RemoteExecutorService) { s.now = now }
}

// NewRemoteExecutorService wires a service against the supplied
// dependencies. `follow` is the negation of the user-facing `--no-follow`
// flag; M4 honors it as a no-op (FollowExecution lands in M5).
func NewRemoteExecutorService(
	projectRepository proPor.ProjectRepository,
	portalClient *portalclient.PortalClient,
	deviceFlow *portalauth.DeviceFlowClient,
	tokenStore *portalauth.FileTokenStore,
	follow bool,
	opts ...RemoteExecutorOption,
) *RemoteExecutorService {
	svc := &RemoteExecutorService{
		projectRepository: projectRepository,
		portalClient:      portalClient,
		deviceFlow:        deviceFlow,
		tokenStore:        tokenStore,
		follow:            follow,
		stdout:            os.Stdout,
		stderr:            os.Stderr,
		now:               func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

// Run is the single public entry point: it loads vexconfig.yaml, ensures
// the user is authenticated (driving the device flow when needed),
// reconciles the project with the portal, syncs the pipeline definition
// when the portal asks for it, and finally triggers the deploy. Streaming
// of logs is deferred to M5 (the `follow` flag is wired through but
// currently surfaces only the queued message).
func (s *RemoteExecutorService) Run(ctx context.Context, step, environment string) error {
	exists, err := s.projectRepository.Exists()
	if err != nil {
		return err
	}
	if !exists {
		return errors.New(MessageProjectNotInitialized)
	}

	project, err := s.projectRepository.Load()
	if err != nil {
		return fmt.Errorf("load project: %w", err)
	}

	if err := s.ensureToken(ctx); err != nil {
		return err
	}

	cgResp, err := s.portalClient.CreateOrGetProject(ctx, buildCreateOrGetRequest(project))
	if err != nil {
		return s.translateError(err)
	}

	if cgResp.Authoritative != nil {
		changed, err := s.applyAuthoritative(project, cgResp.Authoritative)
		if err != nil {
			return fmt.Errorf("apply authoritative: %w", err)
		}
		if changed {
			fmt.Fprintln(s.stdout, "vexconfig.yaml updated from portal (project already exists with different values)")
		}
	}

	if cgResp.NeedsSync {
		fmt.Fprintln(s.stdout, "Syncing pipeline definition with portal...")
		if err := s.portalClient.SyncPipeline(ctx, cgResp.PipelineID); err != nil {
			return fmt.Errorf("sync pipeline: %w", s.translateError(err))
		}
	}

	tdResp, err := s.portalClient.TriggerDeploy(ctx, portalclient.TriggerDeployRequest{
		PipelineID:  cgResp.PipelineID,
		Environment: environment,
		Step:        step,
		Version:     "", // vexd computes it from project git history
	})
	if err != nil {
		return s.translateError(err)
	}

	fmt.Fprintf(s.stdout, "Execution %s queued. Follow at %s\n", tdResp.ExecutionID, tdResp.PortalURL)

	if !s.follow {
		return nil
	}
	// FollowExecution / SSE streaming is part of M5. We surface the
	// portal URL so the user can watch the run from the browser today.
	fmt.Fprintln(s.stdout, "Live log streaming will arrive in M5. Open the portal URL above to follow progress.")
	return nil
}

// ensureToken loads the persisted token, falling back to the device-code
// flow when none is found. Any other load error (corrupt file, IO) is
// surfaced to the caller without re-running the flow.
func (s *RemoteExecutorService) ensureToken(ctx context.Context) error {
	if _, err := s.tokenStore.Load(); err == nil {
		return nil
	} else if !errors.Is(err, portalauth.ErrTokenNotFound) {
		return fmt.Errorf("load credentials: %w", err)
	}
	fmt.Fprintln(s.stdout, "Not authenticated yet. Starting browser-based login...")
	return s.runDeviceFlow(ctx)
}

// runDeviceFlow drives the OAuth Device Authorization Grant against the
// portal and persists the resulting token. It mirrors the body of the
// `vex auth login` subcommand but lives here so the remote executor is
// self-sufficient (no transitive dependency on cmd/).
func (s *RemoteExecutorService) runDeviceFlow(ctx context.Context) error {
	device, err := s.deviceFlow.Start(ctx)
	if err != nil {
		return fmt.Errorf("start device flow: %w", err)
	}

	fmt.Fprintln(s.stdout, "Open the following URL in your browser:")
	fmt.Fprintf(s.stdout, "  %s\n", device.VerificationURIComplete)
	fmt.Fprintf(s.stdout, "Or visit %s and enter the code: %s\n",
		device.VerificationURI, device.UserCode)

	if err := portalauth.OpenBrowser(device.VerificationURIComplete); err != nil {
		fmt.Fprintf(s.stderr, "(could not open browser automatically: %v)\n", err)
	}

	interval := time.Duration(device.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}

	fmt.Fprintln(s.stdout, "Waiting for approval...")
	tokenResp, err := s.deviceFlow.Poll(ctx, device.DeviceCode, interval)
	if err != nil {
		switch {
		case errors.Is(err, portalauth.ErrAccessDenied):
			return errors.New("access denied: portal rejected this device. Run again to retry")
		case errors.Is(err, portalauth.ErrDeviceCodeExpired):
			return errors.New("device code expired before approval. Run again to retry")
		case errors.Is(err, context.Canceled):
			return err
		default:
			return fmt.Errorf("poll device token: %w", err)
		}
	}

	now := s.now()
	token := portalauth.Token{
		AccessToken: tokenResp.AccessToken,
		TokenType:   tokenResp.TokenType,
		ObtainedAt:  now,
		ExpiresAt:   now.Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}
	if err := s.tokenStore.Save(token); err != nil {
		return fmt.Errorf("persist token: %w", err)
	}
	fmt.Fprintln(s.stdout, "Authenticated.")
	return nil
}

// applyAuthoritative compares the portal-canonical view with the local
// aggregate, mutates the in-memory aggregate via HydrateProject and
// persists the result. It returns whether any field actually changed so
// the caller can decide whether to surface the rewrite to the user.
//
// The aggregate's value objects are immutable, so we rebuild Pipeline,
// Runtime and ProjectData from scratch; HydrateProject lets us bypass
// NewProject's invariants because the data we receive is the source of
// truth.
func (s *RemoteExecutorService) applyAuthoritative(project *aggregates.Project, auth *portalclient.AuthoritativeData) (bool, error) {
	currentPipeline := project.Pipeline()
	currentRuntime := project.Runtime()
	currentData := project.Data()

	pipelineChanged := currentPipeline.URL() != auth.Pipeline.URL || currentPipeline.Ref() != auth.Pipeline.Ref
	runtimeChanged := currentRuntime.Image().Image() != auth.Pipeline.RuntimeImage || currentRuntime.Image().Tag() != auth.Pipeline.RuntimeTag
	dataChanged := currentData.URL() != auth.Project.RepoURL || currentData.Ref() != auth.Project.RepoRef

	if !pipelineChanged && !runtimeChanged && !dataChanged {
		return false, nil
	}

	if pipelineChanged {
		newPipeline, err := comVos.NewPipeline(auth.Pipeline.URL, auth.Pipeline.Ref)
		if err != nil {
			return false, fmt.Errorf("rebuild pipeline VO: %w", err)
		}
		project.SetPipeline(newPipeline)
	}

	if runtimeChanged {
		newImage, err := comVos.NewImage(auth.Pipeline.RuntimeImage, auth.Pipeline.RuntimeTag)
		if err != nil {
			return false, fmt.Errorf("rebuild runtime image VO: %w", err)
		}
		newRuntime := proVos.NewRuntime(
			proVos.WithImage(newImage),
			proVos.WithVolumes(currentRuntime.Volumes()),
			proVos.WithEnv(currentRuntime.Env()),
			proVos.WithArgs(currentRuntime.Args()),
		)
		project.SetRuntime(newRuntime)
	}

	if dataChanged {
		newData, err := proVos.NewProjectData(
			currentData.Name(),
			currentData.Organization(),
			currentData.Team(),
			currentData.Description(),
			auth.Project.RepoURL,
			auth.Project.RepoRef,
		)
		if err != nil {
			return false, fmt.Errorf("rebuild project data VO: %w", err)
		}
		// ProjectData lives on the aggregate, but Project has no SetData.
		// HydrateProject bypasses the invariants: the values come from
		// the portal which is the source of truth in this branch.
		updated := aggregates.HydrateProject(project.ID(), newData, project.Pipeline(), project.Runtime())
		// Mutate the original pointer so the caller's reference stays
		// valid by reassigning the dereferenced struct.
		*project = *updated
	}

	if err := s.projectRepository.Save(project); err != nil {
		return false, fmt.Errorf("save project: %w", err)
	}
	return true, nil
}

// translateError rewrites portalclient sentinel errors into messages
// suitable for terminal display. Anything else is passed through.
func (s *RemoteExecutorService) translateError(err error) error {
	switch {
	case errors.Is(err, portalclient.ErrUserConcurrencyLimit):
		return errors.New("you have too many running executions (limit: 3). Wait or cancel one with 'vex execution cancel <id>'")
	case errors.Is(err, portalclient.ErrGlobalCapacityReached):
		var httpErr *portalclient.HTTPError
		if errors.As(err, &httpErr) && httpErr.RetryAfter > 0 {
			return fmt.Errorf("server is at capacity. Retry in %ds", httpErr.RetryAfter)
		}
		return errors.New("server is at capacity. Retry shortly")
	case errors.Is(err, portalclient.ErrUnauthorized):
		return errors.New("portal rejected the saved credentials. Run 'vex auth login' to refresh")
	case errors.Is(err, portalclient.ErrForbidden):
		return errors.New("portal denied access to this project (membership required)")
	case errors.Is(err, portalclient.ErrFlyAPIFailure):
		return errors.New("portal could not start the runner machine. Retry shortly")
	}
	return err
}

// buildCreateOrGetRequest snapshots the relevant fields of the project
// aggregate into the wire-format expected by §6.4.
func buildCreateOrGetRequest(project *aggregates.Project) portalclient.CreateOrGetProjectRequest {
	data := project.Data()
	pipeline := project.Pipeline()
	runtime := project.Runtime()
	return portalclient.CreateOrGetProjectRequest{
		Project: portalclient.ProjectPayload{
			ID:           project.ID().String(),
			Name:         data.Name(),
			Team:         data.Team(),
			Organization: data.Organization(),
			Description:  data.Description(),
			RepoURL:      data.URL(),
			RepoRef:      data.Ref(),
		},
		Pipeline: portalclient.PipelinePayload{
			URL:          pipeline.URL(),
			Ref:          pipeline.Ref(),
			RuntimeImage: runtime.Image().Image(),
			RuntimeTag:   runtime.Image().Tag(),
		},
	}
}

// Compile-time contract check.
var _ Runner = (*RemoteExecutorService)(nil)
