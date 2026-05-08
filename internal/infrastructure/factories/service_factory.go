package factories

import (
	"net/http"
	"os"
	"path/filepath"
	"time"

	app "github.com/jairoprogramador/vex/internal/application"
	docSer "github.com/jairoprogramador/vex/internal/domain/docker/services"
	proPor "github.com/jairoprogramador/vex/internal/domain/project/ports"
	"github.com/jairoprogramador/vex/internal/infrastructure/architecture"
	"github.com/jairoprogramador/vex/internal/infrastructure/common"
	"github.com/jairoprogramador/vex/internal/infrastructure/docker"
	"github.com/jairoprogramador/vex/internal/infrastructure/git"
	"github.com/jairoprogramador/vex/internal/infrastructure/portalauth"
	"github.com/jairoprogramador/vex/internal/infrastructure/portalclient"
	"github.com/jairoprogramador/vex/internal/infrastructure/project"
)

// AuthDependencies bundles the wiring needed by the `vex auth` subcommands.
// It is built once per command invocation so the CLI can share a single
// HTTP client across the device-flow client and the whoami request.
type AuthDependencies struct {
	PortalURL    string
	HTTPClient   *http.Client
	DeviceClient *portalauth.DeviceFlowClient
	TokenStore   *portalauth.FileTokenStore
}

type ServiceFactory interface {
	// BuildRunner returns the deploy orchestrator selected by `remote`.
	// It is the entry point used by the root command for the
	// `vex <step> [env]` flow.
	BuildRunner(remote, follow bool) (app.Runner, error)
	BuildLocalExecutor() (*app.LocalExecutorService, error)
	BuildRemoteExecutor(follow bool) (*app.RemoteExecutorService, error)
	BuildInitialize() (*app.InitializeService, error)
	BuildArchitecture() (*app.ArchitectureService, error)
	BuildAuth() (*AuthDependencies, error)
	BuildPortalClient() (*portalclient.PortalClient, error)
}

type serviceFactory struct{}

func NewServiceFactory() ServiceFactory {
	return &serviceFactory{}
}

func (f *serviceFactory) BuildInitialize() (*app.InitializeService, error) {
	projectPath, err := f.getProjectPath()
	if err != nil {
		return nil, err
	}
	projectRepository, err := f.getProjectRepository(projectPath)
	if err != nil {
		return nil, err
	}

	inputService := common.NewSurveyUserInputService()
	versionService := project.NewHttpVersion()
	levelRepository := architecture.NewCacheLevelRepository()
	questionRepository := architecture.NewCacheQuestionRepository()
	templateRepository := architecture.NewCacheTemplateRepository(
		f.templateCachePath(), f.templateRemoteURL())
	gitInfo := git.NewShellGitInfo()
	return app.NewInitializeService(
		filepath.Base(projectPath), projectPath, projectRepository, inputService,
		versionService, levelRepository, questionRepository, templateRepository, gitInfo), nil
}

// BuildRunner picks between the local Docker executor and the remote
// portal-driven executor based on the `--remote` flag (or the
// VEX_MODE=remote env var, resolved by the caller).
func (f *serviceFactory) BuildRunner(local, follow bool) (app.Runner, error) {
	if local {
		return f.BuildLocalExecutor()
	}
	return f.BuildRemoteExecutor(follow)
}

// BuildLocalExecutor wires the legacy Docker-based executor (kept for the
// non-remote branch of the CLI).
func (f *serviceFactory) BuildLocalExecutor() (*app.LocalExecutorService, error) {
	projectPath, err := f.getProjectPath()
	if err != nil {
		return nil, err
	}
	projectRepository, err := f.getProjectRepository(projectPath)
	if err != nil {
		return nil, err
	}

	cmdExecutor := docker.NewShellExecutor()
	imageService := docSer.NewImageBuilder()
	containerService := docSer.NewContainerBuilder()

	return app.NewLocalExecutorService(
		projectRepository, cmdExecutor, imageService, containerService), nil
}

// BuildRemoteExecutor wires the portal-driven executor used by
// `vex <step> [env] --remote`. The `follow` parameter is the negation of
// `--no-follow`; M4 honors it as a no-op (FollowExecution lands in M5).
func (f *serviceFactory) BuildRemoteExecutor(follow bool) (*app.RemoteExecutorService, error) {
	projectPath, err := f.getProjectPath()
	if err != nil {
		return nil, err
	}
	projectRepository, err := f.getProjectRepository(projectPath)
	if err != nil {
		return nil, err
	}

	tokenStore, err := portalauth.NewFileTokenStore()
	if err != nil {
		return nil, err
	}

	portalURL := portalauth.PortalURL()
	httpClient := &http.Client{Timeout: 30 * time.Second}
	deviceFlow := portalauth.NewDeviceFlowClient(portalURL)
	client := portalclient.NewPortalClient(portalURL, tokenStore, httpClient)

	return app.NewRemoteExecutorService(
		projectRepository, client, deviceFlow, tokenStore, follow,
	), nil
}

// BuildPortalClient returns a portal HTTP client backed by the persisted
// credentials file. It is used by sub-commands that need the portal but
// not the full executor flow (e.g. `vex execution cancel`).
func (f *serviceFactory) BuildPortalClient() (*portalclient.PortalClient, error) {
	tokenStore, err := portalauth.NewFileTokenStore()
	if err != nil {
		return nil, err
	}
	httpClient := &http.Client{Timeout: 30 * time.Second}
	return portalclient.NewPortalClient(portalauth.PortalURL(), tokenStore, httpClient), nil
}

func (f *serviceFactory) BuildArchitecture() (*app.ArchitectureService, error) {
	projectPath, err := f.getProjectPath()
	if err != nil {
		return nil, err
	}
	projectRepository, err := f.getProjectRepository(projectPath)
	if err != nil {
		return nil, err
	}

	questionRepository := architecture.NewCacheQuestionRepository()
	levelRepository := architecture.NewCacheLevelRepository()
	templateRepository := architecture.NewCacheTemplateRepository(
		f.templateCachePath(), f.templateRemoteURL())
	inputService := common.NewSurveyUserInputService()
	return app.NewArchitectureService(
		questionRepository, levelRepository,
		templateRepository, projectRepository, inputService), nil
}

// BuildAuth wires the dependencies for the `vex auth` subcommands. The
// portal URL is resolved by portalauth.PortalURL (env VEX_PORTAL_URL with
// a sensible default), and the credentials file lives under the
// platform-default config directory.
func (f *serviceFactory) BuildAuth() (*AuthDependencies, error) {
	tokenStore, err := portalauth.NewFileTokenStore()
	if err != nil {
		return nil, err
	}

	portalURL := portalauth.BackendURL()
	httpClient := &http.Client{Timeout: 30 * time.Second}

	return &AuthDependencies{
		PortalURL:    portalURL,
		HTTPClient:   httpClient,
		DeviceClient: portalauth.NewDeviceFlowClient(portalURL),
		TokenStore:   tokenStore,
	}, nil
}

func (f *serviceFactory) getProjectRepository(projectPath string) (proPor.ProjectRepository, error) {
	projectRepository := project.NewYAMLProjectRepository(projectPath)
	return projectRepository, nil
}

func (f *serviceFactory) getProjectPath() (string, error) {
	return os.Getwd()
}

func (f *serviceFactory) templateCachePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".vex", "templates.json")
}

func (f *serviceFactory) templateRemoteURL() string {
	if url := os.Getenv("VEX_STORE_TEMPLATE"); url != "" {
		return url
	}
	return "https://raw.githubusercontent.com/jairoprogramador/vex-template-store/main/templates.json"
}
