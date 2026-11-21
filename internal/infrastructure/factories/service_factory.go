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
	arqPor "github.com/jairoprogramador/vex/internal/domain/architecture/ports"
	proPor "github.com/jairoprogramador/vex/internal/domain/project/ports"
	"github.com/jairoprogramador/vex/internal/infrastructure/architecture"
	"github.com/jairoprogramador/vex/internal/infrastructure/bootstrap"
	"github.com/jairoprogramador/vex/internal/infrastructure/cloud/azure"
	"github.com/jairoprogramador/vex/internal/infrastructure/cloudsession"
	"github.com/jairoprogramador/vex/internal/infrastructure/common"
	"github.com/jairoprogramador/vex/internal/infrastructure/console"
	"github.com/jairoprogramador/vex/internal/infrastructure/docker"
	"github.com/jairoprogramador/vex/internal/infrastructure/engine"
	"github.com/jairoprogramador/vex/internal/infrastructure/git"
	"github.com/jairoprogramador/vex/internal/infrastructure/journal"
	"github.com/jairoprogramador/vex/internal/infrastructure/portalauth"
	"github.com/jairoprogramador/vex/internal/infrastructure/portalclient"
	"github.com/jairoprogramador/vex/internal/infrastructure/portalcloud"
	"github.com/jairoprogramador/vex/internal/infrastructure/project"
	"github.com/jairoprogramador/vex/internal/infrastructure/seen"
	"github.com/jairoprogramador/vex/internal/infrastructure/sigstore"
)

// AuthDependencies bundles the wiring needed by the `vex` subcommands.
// It is built once per command invocation so the CLI can share a single
// HTTP client across the device-flow client and the whoami request.
type AuthDependencies struct {
	PortalURL    string
	HTTPClient   *http.Client
	DeviceClient *portalauth.DeviceFlowClient
	TokenStore   portalauth.TokenStore
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
	BuildBootstrapService() (*app.BootstrapService, error)
	BuildDisconnectService() (*app.DisconnectService, error)
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
	notify := console.NewPresenter(os.Stdout, os.Stderr)
	gitInfo := git.NewShellGitInfo()
	return app.NewInitializeService(
		filepath.Base(projectPath), projectPath, projectRepository, inputService,
		versionService, levelRepository, questionRepository, f.templateRepository(notify), gitInfo,
		cloudAdapters(), notify), nil
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
	projects  proPor.ProjectRepository
	// cacheDir es <caché del usuario>/vex: lo que hay ahí se puede borrar y se rehace.
	cacheDir string
}

// cloudAdapters es el registro de nubes que esta versión de vex conoce. Añadir una nube es añadirla aquí.
func cloudAdapters() app.CloudAdapters {
	return app.NewCloudAdapters(azure.New())
}

// CloudErrorCatalogs son los errores que explican las nubes conocidas, para quien muestra un error.
func CloudErrorCatalogs() []app.ErrorCatalog { return cloudAdapters().ErrorCatalogs() }

// CloudErrorHints son las instrucciones con que las nubes conocidas explican sus errores típicos, para quien
// muestra la salida de un intento sin saber con qué nube corrió (`vex log --failed`).
func CloudErrorHints() []app.ErrorHint {
	return cloudAdapters().ErrorHints()
}

// cloudKit es lo que necesita quien ejecuta pasos de un proyecto con cloud.credentials: portal.
type cloudKit struct {
	credentials *cloudsession.Credentials
	preparer    *app.CloudPreparer
	// verifier comprueba la firma de la imagen de runtime antes de que reciba credenciales.
	verifier app.ImageVerifier
}

// buildCloudKit arma la fuente de credenciales del portal y su preparación. Construirlo no toca el portal ni
// el llavero: sin cloud.credentials: portal nada de esto llega a usarse.
func (f *serviceFactory) buildCloudKit(kit *localKit, notify app.Notifier) (*cloudKit, error) {
	tokenStore, err := portalauth.NewDefaultTokenStore()
	if err != nil {
		return nil, err
	}
	backendURL := portalauth.BackendURL()
	client := portalclient.NewPortalClient(backendURL, tokenStore, &http.Client{Timeout: 30 * time.Second})
	portal := portalcloud.New(client)
	adapters := cloudAdapters()

	// La misma sesión que `vex login`: si no hay una válida, abre el navegador en el portal.
	login := portalauth.Login{
		Flow:   portalauth.CLIFlowConfig{Client: portalauth.NewDeviceFlowClient(backendURL), Store: tokenStore},
		Notice: "🔐 Necesitas iniciar sesión en vex.",
		Done:   "Sesión iniciada.",
	}
	// El directorio de credenciales va aparte del espacio de trabajo del motor.
	credentialsDir := filepath.Join(kit.cacheDir, "credenciales")
	return &cloudKit{
		// Hoy el portal es a la vez la fuente de conexiones y la de tokens; en la ejecución remota la segunda
		// será el runner.
		credentials: cloudsession.NewCredentials(portal, portal, adapters, credentialsDir, notify.Warn),
		preparer: app.NewCloudPreparer(
			login, client, kit.projects, portal, bootstrap.Reader{}, adapters, docker.NewImagePuller(), notify,
		).WithTemplateCatalog(templateCatalog(notify)).WithChangeApprover(portalChangeApprover()),
		verifier: sigstore.NewImageVerifier(sigstoreDir(), sigstore.RemoteRegistry{}, !devBuild()),
	}, nil
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
		engine:   engine.NewDockerEngine(),
		recents:  app.NewRecentsService(seen.NewFileStore(vexCache)),
		projects: projectRepository,
		cacheDir: vexCache,
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
	cloud, err := f.buildCloudKit(kit, presenter)
	if err != nil {
		return nil, err
	}
	guard := app.NewPipelineGuard(kit.engine, kit.recents)
	options := []app.LocalExecutorOption{
		app.WithAttemptRecorder(kit.recents), app.WithErrorExplainer(guard),
		app.WithPortalCredentials(cloud.credentials), app.WithCloudPreparer(cloud.preparer),
		app.WithImageVerifier(cloud.verifier),
		app.WithTimingsFile(filepath.Join(kit.cacheDir, "tiempos.jsonl")),
	}
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
	cloud, err := f.buildCloudKit(kit, presenter)
	if err != nil {
		return nil, err
	}
	return app.NewRollbackService(kit.workspace, kit.engine, presenter, kit.recents,
		app.WithRollbackPortalCredentials(cloud.credentials), app.WithRollbackCloudPreparer(cloud.preparer),
		app.WithRollbackImageVerifier(cloud.verifier)), nil
}

// cloudSetupKit es lo que comparten `vex bootstrap` y `vex disconnect`: el proyecto, la sesión de vex, el
// portal como administrador de conexiones y la constancia local de lo aceptado.
type cloudSetupKit struct {
	workspace *app.Workspace
	preparer  *app.CloudPreparer
	adapters  app.CloudAdapters
	admin     app.CloudConnectionAdmin
	journal   app.BootstrapJournal
	notify    app.Notifier
}

// adminTimeout cubre el tiempo que el portal dedica a verificar una conexión dentro de una sola llamada.
const adminTimeout = 2 * time.Minute

func (f *serviceFactory) buildCloudSetupKit() (*cloudSetupKit, error) {
	// Lo que se anuncia va a stderr: stdout es la vista previa y los comandos que se imprimen.
	notify := console.NewPresenter(os.Stderr, os.Stderr)
	kit, err := f.buildLocalKit(notify)
	if err != nil {
		return nil, err
	}
	tokenStore, err := portalauth.NewDefaultTokenStore()
	if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user home: %w", err)
	}
	backendURL := portalauth.BackendURL()
	client := portalclient.NewPortalClient(backendURL, tokenStore, &http.Client{Timeout: adminTimeout})
	portal := portalcloud.New(client)
	adapters := cloudAdapters()
	login := portalauth.Login{
		Flow:   portalauth.CLIFlowConfig{Client: portalauth.NewDeviceFlowClient(backendURL), Store: tokenStore},
		Notice: "🔐 Necesitas iniciar sesión en vex.",
		Done:   "Sesión iniciada.",
	}
	return &cloudSetupKit{
		workspace: kit.workspace,
		// Sin descarga de imagen: estos comandos no crean contenedores.
		preparer: app.NewCloudPreparer(login, client, kit.projects, portal, bootstrap.Reader{}, adapters, nil, notify).
			WithTemplateCatalog(templateCatalog(notify)).WithChangeApprover(portalChangeApprover()),
		adapters: adapters,
		admin:    portal,
		// Junto al historial del motor: es constancia, no caché.
		journal: journal.NewFile(filepath.Join(home, ".vex", "nube", "aceptaciones.jsonl")),
		notify:  notify,
	}, nil
}

// BuildBootstrapService wires `vex bootstrap`: el bootstrap del pipeline, aplicado con la sesión de la nube de
// quien lo ejecuta. El emisor de tokens es el del binario.
func (f *serviceFactory) BuildBootstrapService() (*app.BootstrapService, error) {
	kit, err := f.buildCloudSetupKit()
	if err != nil {
		return nil, err
	}
	return app.NewBootstrapService(kit.workspace, kit.preparer, bootstrap.Reader{}, kit.adapters, kit.admin,
		kit.journal, kit.notify, portalauth.OIDCIssuer()), nil
}

// BuildDisconnectService wires `vex disconnect`.
func (f *serviceFactory) BuildDisconnectService() (*app.DisconnectService, error) {
	kit, err := f.buildCloudSetupKit()
	if err != nil {
		return nil, err
	}
	return app.NewDisconnectService(kit.workspace, kit.preparer, kit.adapters, kit.admin, kit.journal, kit.notify,
		portalauth.OIDCIssuer()), nil
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

	tokenStore, err := portalauth.NewDefaultTokenStore()
	if err != nil {
		return nil, err
	}

	portalURL := portalauth.BackendURL()
	httpClient := &http.Client{Timeout: 30 * time.Second}
	deviceFlow := portalauth.NewDeviceFlowClient(portalURL)
	client := portalclient.NewPortalClient(portalURL, tokenStore, httpClient)

	return app.NewRemoteExecutorService(
		projectRepository, client, deviceFlow, tokenStore, follow, app.WithChangeApprover(portalChangeApprover()),
	), nil
}

// BuildPortalClient returns a portal HTTP client backed by the persisted
// credentials file. It is used by sub-commands that need the portal but
// not the full executor flow (e.g. `vex execution cancel`).
func (f *serviceFactory) BuildPortalClient() (*portalclient.PortalClient, error) {
	tokenStore, err := portalauth.NewDefaultTokenStore()
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
	inputService := common.NewSurveyUserInputService()
	return app.NewArchitectureService(
		questionRepository, levelRepository,
		f.templateRepository(console.NewPresenter(os.Stdout, os.Stderr)), projectRepository, inputService,
		cloudAdapters()), nil
}

// BuildAuth wires the dependencies for the `vex` subcommands.
func (f *serviceFactory) BuildAuth() (*AuthDependencies, error) {
	tokenStore, err := portalauth.NewDefaultTokenStore()
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

// buildChannel dice cómo se construyó este binario. Por defecto es un release, con todas sus exigencias: un
// `go build` o un `go install` sin más no abre ningún atajo. El modo de desarrollo hay que pedirlo al enlazar:
//
//	go build -ldflags "-X github.com/jairoprogramador/vex/internal/infrastructure/factories.buildChannel=dev" ./cmd/vex
var buildChannel = "release"

// devBuild dice si este binario se construyó, a propósito, para desarrollo.
func devBuild() bool { return buildChannel == "dev" }

const (
	// envOwnCatalog apunta `vex init` y `vex arq` a un catálogo propio, que no lleva la firma de vex.
	envOwnCatalog = "VEX_STORE_TEMPLATE"
	// envUnsignedTemplates deja probar, solo en builds de desarrollo, plantillas de vex que el catálogo firmado
	// aún no publica: se tratan como plantillas sin firma.
	envUnsignedTemplates = "VEX_DEV_UNSIGNED_TEMPLATES"
)

func vexHome() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".vex")
}

// sigstoreDir guarda la raíz de confianza de Sigstore entre ejecuciones.
func sigstoreDir() string { return filepath.Join(vexHome(), "sigstore") }

// officialCatalog es el catálogo firmado de vex: nada de él se usa sin verificar su firma.
func officialCatalog() *architecture.CacheTemplateRepository {
	return architecture.NewCacheTemplateRepository(
		filepath.Join(vexHome(), "pipelines.json"), architecture.OfficialCatalogURL,
		sigstore.NewCatalogVerifier(sigstoreDir(), !devBuild()))
}

// templateRepository es de donde `vex init` y `vex arq` eligen plantilla: el catálogo firmado, o el propio de
// quien define VEX_STORE_TEMPLATE, que se guarda aparte para no mezclarse nunca con el verificado.
func (f *serviceFactory) templateRepository(notify app.Notifier) arqPor.TemplateRepository {
	source := os.Getenv(envOwnCatalog)
	if source == "" {
		return officialCatalog()
	}
	notify.Warn(fmt.Sprintf("%s está definida: se usa un catálogo propio (%s) sin verificar su firma. "+
		"Sus plantillas no llevan la firma de vex: las revisas tú.", envOwnCatalog, source))
	return architecture.NewCacheTemplateRepository(filepath.Join(vexHome(), "pipelines-propio.json"), source, nil)
}

// templateCatalog es contra qué se compara el bootstrap de un pipeline antes de usarlo: siempre el catálogo
// firmado, se elija la plantilla de donde se elija. Solo un build de desarrollo puede prescindir de él, y lo dice.
func templateCatalog(notify app.Notifier) app.TemplateCatalog {
	if os.Getenv(envUnsignedTemplates) == "" {
		return officialCatalog()
	}
	if !devBuild() {
		notify.Warn(envUnsignedTemplates + " solo existe en builds de desarrollo: se ignora y las plantillas de vex se comparan con el catálogo firmado.")
		return officialCatalog()
	}
	notify.Warn("MODO DE DESARROLLO: " + envUnsignedTemplates + " está definida y NO se consulta el catálogo firmado. " +
		"Todas las plantillas, también las de vex, se tratan como plantillas sin firma. No lo uses con una nube real que te importe.")
	return nil
}

// portalChangeApprover pregunta en la terminal antes de adoptar un cambio de pipeline o de imagen que proponga el
// portal. Sin terminal (un script, un CI) nadie puede aceptarlo y la ejecución se detiene.
func portalChangeApprover() app.ChangeApprover {
	return console.NewConfirmer(os.Stdin, os.Stderr).ApprovePortalChanges
}
