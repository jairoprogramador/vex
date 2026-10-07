package factories

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	app "github.com/jairoprogramador/vex/internal/application"
	"github.com/jairoprogramador/vex/internal/config"
	proPor "github.com/jairoprogramador/vex/internal/domain/project/ports"
	"github.com/jairoprogramador/vex/internal/infrastructure/architecture"
	"github.com/jairoprogramador/vex/internal/infrastructure/common"
	"github.com/jairoprogramador/vex/internal/infrastructure/console"
	"github.com/jairoprogramador/vex/internal/infrastructure/docker"
	"github.com/jairoprogramador/vex/internal/infrastructure/engine"
	"github.com/jairoprogramador/vex/internal/infrastructure/git"
	"github.com/jairoprogramador/vex/internal/infrastructure/portalauth"
	"github.com/jairoprogramador/vex/internal/infrastructure/portalclient"
	"github.com/jairoprogramador/vex/internal/infrastructure/project"
	"github.com/jairoprogramador/vex/internal/infrastructure/seen"
)

// AuthDependencies bundles the wiring needed by the `vex` subcommands.
// It is built once per command invocation so the CLI can share a single
// HTTP client across the device-flow client and the whoami request.
type AuthDependencies struct {
	PortalURL    string
	HTTPClient   *http.Client
	DeviceClient *portalauth.DeviceFlowClient
	TokenStore   *portalauth.FileTokenStore
}

type ServiceFactory interface {
	// BuildRunner retorna el orquestador de deploy seleccionado según mode.
	// Es el punto de entrada usado por el root command para el flujo
	// `vex <step> [env]`.
	BuildRunner(mode config.ExecutionMode, follow, preflight bool) (app.Runner, error)
	BuildLocalExecutor(preflight bool) (*app.LocalExecutorService, error)
	BuildQueryService() (*app.QueryService, error)
	BuildPipelineService() (*app.PipelineService, error)
	BuildDiagnosisService() (*app.DiagnosisService, error)
	BuildAbandonService() (*app.AbandonService, error)
	BuildRollbackService() (*app.RollbackService, error)
	BuildReleaseService() (*app.ReleaseService, error)
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

// BuildRunner elige entre el executor Docker local y el executor remoto
// vía portal, basándose en el modo resuelto por el caller (root command).
func (f *serviceFactory) BuildRunner(mode config.ExecutionMode, follow, preflight bool) (app.Runner, error) {
	if mode == config.ModeRemote {
		return f.BuildRemoteExecutor(follow)
	}
	return f.BuildLocalExecutor(preflight)
}

// localKit son las piezas que comparten todos los comandos que hablan con el motor local.
type localKit struct {
	workspace *app.Workspace
	engine    *engine.DockerEngine
	recents   *app.RecentsService
}

// buildLocalKit arma lo común del modo local: el proyecto, la imagen, los clones, el motor y la memoria. notify
// es por donde se avisa de lo que tarda (clonar, construir la imagen).
func (f *serviceFactory) buildLocalKit(notify app.Notifier) (*localKit, error) {
	projectPath, err := f.getProjectPath()
	if err != nil {
		return nil, err
	}
	projectRepository, err := f.getProjectRepository(projectPath)
	if err != nil {
		return nil, err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user home: %w", err)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user cache dir: %w", err)
	}
	// Lo que está en ~/.vex es la verdad (historial del motor); lo que está en la
	// caché del usuario se puede borrar y se rehace.
	vexCache := filepath.Join(cache, "vex")

	config := app.LocalExecutorConfig{
		WorkDir:     projectPath,
		StoreDir:    filepath.Join(home, ".vex", "almacen"),
		SpaceDir:    filepath.Join(vexCache, "espacio"),
		MaterialDir: filepath.Join(vexCache, "material"),
		Requester:   git.RequesterName(context.Background(), projectPath),
	}

	return &localKit{
		workspace: app.NewWorkspace(
			projectRepository,
			git.NewSourceCloner(filepath.Join(vexCache, "sources")),
			docker.NewImageBuilder(os.Stderr),
			notify,
			config),
		engine:  engine.NewDockerEngine(),
		recents: app.NewRecentsService(seen.NewFileStore(vexCache)),
	}, nil
}

// BuildLocalExecutor wires the executor that runs vex-engine in a local
// container: it clones project and pipeline into the user cache, mounts them and
// talks JSON-RPC with the engine.
//
// Con preflight, antes de abrir un intento comprueba (sin ejecutar nada) que el paso se puede ejecutar en ese
// ambiente. Sin él, un ambiente o paso inexistente se explica igualmente, pero al fallar.
func (f *serviceFactory) BuildLocalExecutor(preflight bool) (*app.LocalExecutorService, error) {
	presenter := console.NewPresenter(os.Stdout, os.Stderr)
	kit, err := f.buildLocalKit(presenter)
	if err != nil {
		return nil, err
	}
	guard := app.NewPipelineGuard(kit.engine, kit.recents)
	options := []app.LocalExecutorOption{app.WithAttemptRecorder(kit.recents), app.WithErrorExplainer(guard)}
	if preflight {
		options = append(options, app.WithPreflight(guard))
	}
	return app.NewLocalExecutorService(kit.workspace, kit.engine, presenter, options...), nil
}

// BuildPipelineService wires what can be known about the pipeline without running it (`vex envs`, `steps`,
// `check`). Like the queries, it announces what it prepares on stderr.
func (f *serviceFactory) BuildPipelineService() (*app.PipelineService, error) {
	kit, err := f.buildLocalKit(console.NewPresenter(os.Stderr, os.Stderr))
	if err != nil {
		return nil, err
	}
	guard := app.NewPipelineGuard(kit.engine, kit.recents)
	return app.NewPipelineService(kit.workspace, kit.engine, guard, kit.recents), nil
}

// BuildQueryService wires the read-only queries over the engine's history (`vex ls`, `show`, `log`, ...).
// What it announces while preparing (building the image) goes to stderr so it never mixes with the answer.
func (f *serviceFactory) BuildQueryService() (*app.QueryService, error) {
	kit, err := f.buildLocalKit(console.NewPresenter(os.Stderr, os.Stderr))
	if err != nil {
		return nil, err
	}
	return app.NewQueryService(kit.workspace, kit.engine, kit.recents), nil
}

// BuildDiagnosisService wires `vex why`: it only reads the history, so it needs no clones.
func (f *serviceFactory) BuildDiagnosisService() (*app.DiagnosisService, error) {
	kit, err := f.buildLocalKit(console.NewPresenter(os.Stderr, os.Stderr))
	if err != nil {
		return nil, err
	}
	return app.NewDiagnosisService(kit.workspace, kit.engine, kit.recents), nil
}

// BuildAbandonService wires `vex abandon`.
func (f *serviceFactory) BuildAbandonService() (*app.AbandonService, error) {
	kit, err := f.buildLocalKit(console.NewPresenter(os.Stderr, os.Stderr))
	if err != nil {
		return nil, err
	}
	return app.NewAbandonService(kit.workspace, kit.engine, kit.recents), nil
}

// BuildReleaseService wires `vex release`, `protect` and `unprotect`.
func (f *serviceFactory) BuildReleaseService() (*app.ReleaseService, error) {
	kit, err := f.buildLocalKit(console.NewPresenter(os.Stderr, os.Stderr))
	if err != nil {
		return nil, err
	}
	return app.NewReleaseService(kit.workspace, kit.engine, kit.recents), nil
}

// BuildRollbackService wires `vex rollback`: like running a step, it clones project and pipeline and shows the
// engine's progress live.
func (f *serviceFactory) BuildRollbackService() (*app.RollbackService, error) {
	presenter := console.NewPresenter(os.Stdout, os.Stderr)
	kit, err := f.buildLocalKit(presenter)
	if err != nil {
		return nil, err
	}
	return app.NewRollbackService(kit.workspace, kit.engine, presenter, kit.recents), nil
}

// BuildRemoteExecutor wires the portal-driven executor used by
// `vex <step> [env] --mode remote`. The `follow` parameter is the negation of
// `--no-follow`: when false, the executor returns as soon as the execution is
// queued instead of streaming its logs.
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

	portalURL := portalauth.BackendURL()
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
	return portalclient.NewPortalClient(portalauth.BackendURL(), tokenStore, httpClient), nil
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

// BuildAuth wires the dependencies for the `vex` subcommands.
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
	return filepath.Join(home, ".vex", "pipelines.json")
}

func (f *serviceFactory) templateRemoteURL() string {
	if url := os.Getenv("VEX_STORE_TEMPLATE"); url != "" {
		return url
	}
	return "https://raw.githubusercontent.com/jairoprogramador/vex-template-store/main/templates.json"
}
