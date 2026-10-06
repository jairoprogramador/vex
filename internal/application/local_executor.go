package application

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jairoprogramador/vex/internal/domain/project/aggregates"
	proPor "github.com/jairoprogramador/vex/internal/domain/project/ports"
)

// Rutas dentro del contenedor. Son parte del contrato con la imagen runtime:
// vexd lee las tres variables VEX_* y los repos se le pasan como rutas locales.
const (
	containerProjectPath  = "/proyecto"
	containerPipelinePath = "/pipeline"
	containerStorePath    = "/vex/almacen"
	containerSpacePath    = "/vex/espacio"
	containerMaterialPath = "/vex/material"
)

// LocalExecutorConfig son los datos del host que el ejecutor necesita y no puede
// deducir: dónde está el proyecto y dónde persiste el motor su estado.
type LocalExecutorConfig struct {
	// WorkDir es el directorio con vexconfig.yaml; es el contexto de build de la imagen.
	WorkDir string
	// StoreDir guarda el historial del motor. Es la verdad: no se borra.
	StoreDir string
	// SpaceDir y MaterialDir son derivables: el motor los rehace en cada intento.
	SpaceDir    string
	MaterialDir string
	// Requester es quien pide la ejecución, para el historial del motor.
	Requester string
}

// LocalExecutorService ejecuta un paso del pipeline con vex-engine en un contenedor
// local: clona proyecto y pipeline, los monta y le pide un intento al motor.
type LocalExecutorService struct {
	projectRepository proPor.ProjectRepository
	sources           SourceCloner
	images            ImageBuilder
	engine            EngineClient
	presenter         ExecutionPresenter
	config            LocalExecutorConfig
}

func NewLocalExecutorService(
	projectRepository proPor.ProjectRepository,
	sources SourceCloner,
	images ImageBuilder,
	engine EngineClient,
	presenter ExecutionPresenter,
	config LocalExecutorConfig,
) *LocalExecutorService {
	return &LocalExecutorService{
		projectRepository: projectRepository,
		sources:           sources,
		images:            images,
		engine:            engine,
		presenter:         presenter,
		config:            config,
	}
}

var _ Runner = (*LocalExecutorService)(nil)

func (s *LocalExecutorService) Run(ctx context.Context, step, environment string) error {
	if environment == "" {
		return ErrEnvironmentRequired
	}

	err := s.run(ctx, step, environment)
	// Ctrl+C durante el clonado, el build o el arranque llega como un error
	// cualquiera de esas herramientas; para quien llama es una cancelación.
	if err != nil && ctx.Err() != nil && !errors.Is(err, ErrAttemptFailed) {
		return ErrAttemptCanceled
	}
	return err
}

func (s *LocalExecutorService) run(ctx context.Context, step, environment string) error {
	project, err := s.loadProject()
	if err != nil {
		return err
	}
	if err := s.prepareHostDirs(); err != nil {
		return err
	}

	projectSource, err := s.clone(ctx, "proyecto", project.Data().URL(), project.Data().Ref())
	if err != nil {
		return err
	}
	pipelineSource, err := s.clone(ctx, "pipeline", project.Pipeline().URL(), project.Pipeline().Ref())
	if err != nil {
		return err
	}

	image, err := s.resolveImage(ctx, project)
	if err != nil {
		return err
	}

	spec := s.containerSpec(image, project, projectSource, pipelineSource)
	request := s.attemptRequest(project, step, environment, projectSource, pipelineSource)

	result, err := s.engine.Attempt(ctx, spec, request, s.presenter.Event)
	if err != nil {
		return withEnvironment(asCanceledIfCanceled(err), environment)
	}
	s.presenter.Result(result)
	return s.outcome(ctx, spec, result)
}

func (s *LocalExecutorService) loadProject() (*aggregates.Project, error) {
	exists, err := s.projectRepository.Exists()
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.New(MessageProjectNotInitialized)
	}
	return s.projectRepository.Load()
}

// prepareHostDirs crea los directorios que se montan: Docker los crearía como
// root y el motor exige que el almacén exista.
func (s *LocalExecutorService) prepareHostDirs() error {
	for _, dir := range []string{s.config.StoreDir, s.config.SpaceDir, s.config.MaterialDir} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("crear directorio %q: %w", dir, err)
		}
	}
	return nil
}

func (s *LocalExecutorService) clone(ctx context.Context, label, url, ref string) (Source, error) {
	s.presenter.Info(fmt.Sprintf("Preparando %s: %s@%s", label, url, ref))
	source, err := s.sources.Ensure(ctx, url, ref)
	if err != nil {
		return Source{}, fmt.Errorf("preparar %s: %w", label, err)
	}
	return source, nil
}

// resolveImage devuelve la imagen a ejecutar: la del registro tal cual, o una
// construida localmente cuando runtime.image apunta a un Dockerfile.
func (s *LocalExecutorService) resolveImage(ctx context.Context, project *aggregates.Project) (string, error) {
	image := project.Runtime().Image()
	if image.TagExplicit() {
		return image.String(), nil
	}

	tag := fmt.Sprintf("%s%s:%s",
		strings.ToLower(project.Data().Name()), project.ID().String()[:6], image.Tag())
	s.presenter.Info(fmt.Sprintf("Construyendo imagen %s desde %s", tag, image.Image()))

	err := s.images.Build(ctx, ImageBuild{
		Tag:        tag,
		Dockerfile: image.Image(),
		ContextDir: s.config.WorkDir,
		Args:       buildArgs(project),
	})
	if err != nil {
		return "", fmt.Errorf("construir imagen: %w", err)
	}
	return tag, nil
}

// buildArgs añade el uid/gid del usuario para que los archivos de los volúmenes
// queden con su dueño (no existen en Windows). Los args del proyecto prevalecen.
func buildArgs(project *aggregates.Project) []BuildArg {
	var args []BuildArg
	if uid, gid := os.Getuid(), os.Getgid(); uid >= 0 && gid >= 0 {
		args = append(args,
			BuildArg{Name: "DEV_GID", Value: fmt.Sprint(gid)},
			BuildArg{Name: "DEV_UID", Value: fmt.Sprint(uid)})
	}
	for _, arg := range project.Runtime().Args() {
		args = withBuildArg(args, BuildArg{Name: arg.Name(), Value: arg.Value()})
	}
	return args
}

func withBuildArg(args []BuildArg, arg BuildArg) []BuildArg {
	for i := range args {
		if args[i].Name == arg.Name {
			args[i] = arg
			return args
		}
	}
	return append(args, arg)
}

func (s *LocalExecutorService) containerSpec(
	image string, project *aggregates.Project, projectSource, pipelineSource Source,
) ContainerSpec {
	mounts := []Mount{
		{Source: projectSource.Path, Target: containerProjectPath, ReadOnly: true},
		{Source: pipelineSource.Path, Target: containerPipelinePath, ReadOnly: true},
		{Source: s.config.StoreDir, Target: containerStorePath},
		{Source: s.config.SpaceDir, Target: containerSpacePath},
		{Source: s.config.MaterialDir, Target: containerMaterialPath},
	}
	for _, volume := range project.Runtime().Volumes() {
		mounts = append(mounts, Mount{Source: volume.Host(), Target: volume.Container()})
	}

	// La imagen puede no fijarlas: el ejecutor las pasa siempre.
	env := []EnvVar{
		{Name: "VEX_ALMACEN", Value: containerStorePath},
		{Name: "VEX_ESPACIO", Value: containerSpacePath},
		{Name: "VEX_MATERIAL", Value: containerMaterialPath},
	}
	return ContainerSpec{Image: image, Mounts: mounts, Env: env}
}

func (s *LocalExecutorService) attemptRequest(
	project *aggregates.Project, step, environment string, projectSource, pipelineSource Source,
) AttemptRequest {
	return AttemptRequest{
		Environment:    environment,
		Requester:      s.config.Requester,
		ProjectSource:  containerProjectPath,
		ProjectCommit:  projectSource.Commit,
		PipelineSource: containerPipelinePath,
		PipelineCommit: pipelineSource.Commit,
		UntilStep:      step,
		Project: ProjectMetadata{
			ID:           project.ID().String(),
			Name:         project.Data().Name(),
			Organization: project.Data().Organization(),
			Team:         project.Data().Team(),
		},
		Secrets: s.secrets(project),
	}
}

// secrets expande las variables de runtime.run.envs con el entorno del host
// (`$ARM_CLIENT_SECRET` o `${ARM_CLIENT_SECRET}`), de modo que el valor real
// nunca se escribe en vexconfig.yaml. Viajan por stdin al motor, no por argv.
func (s *LocalExecutorService) secrets(project *aggregates.Project) map[string]string {
	envs := project.Runtime().Env()
	if len(envs) == 0 {
		return nil
	}
	secrets := make(map[string]string, len(envs))
	for _, env := range envs {
		value := os.Expand(env.Value(), os.Getenv)
		if value == "" {
			s.presenter.Warn(fmt.Sprintf(
				"%s no tiene valor (%q se expandió a vacío en este entorno); no se enviará",
				env.Name(), env.Value()))
			continue
		}
		secrets[env.Name()] = value
	}
	return secrets
}

// outcome convierte el resultado del intento en el error que ve quien llama. Si
// falló, muestra la salida de los comandos fallidos: el progreso no la trae.
func (s *LocalExecutorService) outcome(ctx context.Context, spec ContainerSpec, result AttemptResult) error {
	switch result.Status {
	case AttemptSucceeded:
		return nil
	case AttemptCanceled:
		return ErrAttemptCanceled
	}

	outputs, err := s.engine.Logs(ctx, spec, LogsRequest{AttemptID: result.AttemptID, OnlyFailed: true})
	if err != nil {
		// El fallo del intento es lo importante; no se tapa con el de los logs.
		s.presenter.Warn(fmt.Sprintf("No se pudo leer la salida de los comandos fallidos: %v", err))
		return ErrAttemptFailed
	}
	s.presenter.FailedCommands(outputs)
	return ErrAttemptFailed
}

// withEnvironment completa el ambiente pedido en un error del motor que no lo
// trae, para que el mensaje pueda nombrarlo.
func withEnvironment(err error, environment string) error {
	var engineErr *EngineError
	if errors.As(err, &engineErr) && engineErr.Environment == "" {
		engineErr.Environment = environment
	}
	return err
}

func asCanceledIfCanceled(err error) error {
	var engineErr *EngineError
	if errors.As(err, &engineErr) && engineErr.Kind == EngineCanceled {
		return ErrAttemptCanceled
	}
	return err
}
