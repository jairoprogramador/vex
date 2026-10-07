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

// Rutas dentro del contenedor. Son parte del contrato con la imagen runtime: vexd lee las tres variables VEX_* y
// los repos se le pasan como rutas locales. Las rutas de los repos van siempre iguales: el motor las guarda en
// cada despliegue y un rollback futuro las vuelve a abrir.
const (
	containerProjectPath  = "/proyecto"
	containerPipelinePath = "/pipeline"
	containerStorePath    = "/vex/almacen"
	containerSpacePath    = "/vex/espacio"
	containerMaterialPath = "/vex/material"
)

// LocalExecutorConfig son los datos del host que el CLI necesita y no puede deducir: dónde está el proyecto y
// dónde persiste el motor su estado.
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

// Notifier es por donde el CLI avisa de una etapa de preparación (clonar, construir la imagen) o de algo que no
// impide seguir.
type Notifier interface {
	Info(message string)
	Warn(message string)
}

// Workspace prepara lo que hace falta antes de hablar con el motor: el proyecto, la imagen y el contenedor. Lo
// comparten todos los comandos; cada uno pide solo el contenedor que necesita (las consultas al historial no
// necesitan clonar nada).
type Workspace struct {
	projects proPor.ProjectRepository
	sources  SourceCloner
	images   ImageBuilder
	notify   Notifier
	config   LocalExecutorConfig
}

func NewWorkspace(
	projects proPor.ProjectRepository, sources SourceCloner, images ImageBuilder, notify Notifier, config LocalExecutorConfig,
) *Workspace {
	return &Workspace{projects: projects, sources: sources, images: images, notify: notify, config: config}
}

// HistorySession es lo que toda operación sobre el historial necesita: a qué proyecto pertenece y un contenedor
// con el historial montado.
type HistorySession struct {
	ProjectID string
	Spec      ContainerSpec
}

// OpenHistory prepara una sesión sobre el historial: lee el proyecto, crea las carpetas y resuelve la imagen. No
// clona nada.
func (w *Workspace) OpenHistory(ctx context.Context) (HistorySession, error) {
	project, err := w.LoadProject()
	if err != nil {
		return HistorySession{}, err
	}
	if err := w.PrepareDirs(); err != nil {
		return HistorySession{}, err
	}
	image, err := w.ResolveImage(ctx, project)
	if err != nil {
		return HistorySession{}, err
	}
	return HistorySession{ProjectID: project.ID().String(), Spec: w.HistorySpec(image)}, nil
}

// Requester es quien pide la operación.
func (w *Workspace) Requester() string { return w.config.Requester }

// LoadProject lee vexconfig.yaml.
func (w *Workspace) LoadProject() (*aggregates.Project, error) {
	exists, err := w.projects.Exists()
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.New(MessageProjectNotInitialized)
	}
	return w.projects.Load()
}

// PrepareDirs crea los directorios que se montan: Docker los crearía como root y el motor exige que el almacén
// exista.
func (w *Workspace) PrepareDirs() error {
	for _, dir := range []string{w.config.StoreDir, w.config.SpaceDir, w.config.MaterialDir} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("crear directorio %q: %w", dir, err)
		}
	}
	return nil
}

func (w *Workspace) CloneProject(ctx context.Context, project *aggregates.Project) (Source, error) {
	return w.clone(ctx, "proyecto", project.Data().URL(), project.Data().Ref())
}

func (w *Workspace) ClonePipeline(ctx context.Context, project *aggregates.Project) (Source, error) {
	return w.clone(ctx, "pipeline", project.Pipeline().URL(), project.Pipeline().Ref())
}

func (w *Workspace) clone(ctx context.Context, label, url, ref string) (Source, error) {
	w.notify.Info(fmt.Sprintf("Preparando %s: %s@%s", label, url, ref))
	source, err := w.sources.Ensure(ctx, url, ref)
	if err != nil {
		return Source{}, fmt.Errorf("preparar %s: %w", label, err)
	}
	return source, nil
}

// ResolveImage devuelve la imagen a ejecutar: la del registro tal cual, o una construida localmente cuando
// runtime.image apunta a un Dockerfile.
func (w *Workspace) ResolveImage(ctx context.Context, project *aggregates.Project) (string, error) {
	image := project.Runtime().Image()
	if image.TagExplicit() {
		return image.String(), nil
	}

	tag := fmt.Sprintf("%s%s:%s",
		strings.ToLower(project.Data().Name()), project.ID().String()[:6], image.Tag())
	w.notify.Info(fmt.Sprintf("Construyendo imagen %s desde %s", tag, image.Image()))

	err := w.images.Build(ctx, ImageBuild{
		Tag:        tag,
		Dockerfile: image.Image(),
		ContextDir: w.config.WorkDir,
		Args:       buildArgs(project),
	})
	if err != nil {
		return "", fmt.Errorf("construir imagen: %w", err)
	}
	return tag, nil
}

// buildArgs añade el uid/gid del usuario para que los archivos de los volúmenes queden con su dueño (no existen
// en Windows). Los args del proyecto prevalecen.
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

// La imagen puede no fijar las variables VEX_*: el CLI las pasa siempre.
func (w *Workspace) storeEnv() []EnvVar {
	return []EnvVar{{Name: "VEX_ALMACEN", Value: containerStorePath}}
}

func (w *Workspace) storeMount() Mount {
	return Mount{Source: w.config.StoreDir, Target: containerStorePath}
}

// HistorySpec es el contenedor de las consultas y escrituras que solo tocan el historial: sin clones.
func (w *Workspace) HistorySpec(image string) ContainerSpec {
	return ContainerSpec{Image: image, Mounts: []Mount{w.storeMount()}, Env: w.storeEnv()}
}

// PipelineSpec es el contenedor de lo que lee el pipeline sin ejecutarlo (check, envs, steps).
func (w *Workspace) PipelineSpec(image string, pipeline Source) ContainerSpec {
	return ContainerSpec{
		Image: image,
		Mounts: []Mount{
			{Source: pipeline.Path, Target: containerPipelinePath, ReadOnly: true},
			w.storeMount(),
		},
		Env: w.storeEnv(),
	}
}

// ExecutionSpec es el contenedor de lo que ejecuta comandos (un intento, un rollback).
func (w *Workspace) ExecutionSpec(image string, project *aggregates.Project, projectSource, pipelineSource Source) ContainerSpec {
	mounts := []Mount{
		{Source: projectSource.Path, Target: containerProjectPath, ReadOnly: true},
		{Source: pipelineSource.Path, Target: containerPipelinePath, ReadOnly: true},
		w.storeMount(),
		{Source: w.config.SpaceDir, Target: containerSpacePath},
		{Source: w.config.MaterialDir, Target: containerMaterialPath},
	}
	for _, volume := range project.Runtime().Volumes() {
		mounts = append(mounts, Mount{Source: volume.Host(), Target: volume.Container()})
	}
	env := append(w.storeEnv(),
		EnvVar{Name: "VEX_ESPACIO", Value: containerSpacePath},
		EnvVar{Name: "VEX_MATERIAL", Value: containerMaterialPath})
	return ContainerSpec{Image: image, Mounts: mounts, Env: env}
}

// PipelineRef es el pipeline tal como lo ve el motor dentro del contenedor.
func (w *Workspace) PipelineRef(pipeline Source) PipelineRef {
	return PipelineRef{Source: containerPipelinePath, Commit: pipeline.Commit}
}

// Metadata son los datos del proyecto que el motor expone a los pipelines como variables.
func (w *Workspace) Metadata(project *aggregates.Project) ProjectMetadata {
	return ProjectMetadata{
		ID:           project.ID().String(),
		Name:         project.Data().Name(),
		Organization: project.Data().Organization(),
		Team:         project.Data().Team(),
	}
}

// Secrets expande las variables de runtime.run.envs con el entorno del host (`$ARM_CLIENT_SECRET` o
// `${ARM_CLIENT_SECRET}`), de modo que el valor real nunca se escribe en vexconfig.yaml. Viajan por stdin al
// motor, no por argv.
func (w *Workspace) Secrets(project *aggregates.Project) map[string]string {
	envs := project.Runtime().Env()
	if len(envs) == 0 {
		return nil
	}
	secrets := make(map[string]string, len(envs))
	for _, env := range envs {
		value := os.Expand(env.Value(), os.Getenv)
		if value == "" {
			w.notify.Warn(fmt.Sprintf(
				"%s no tiene valor (%q se expandió a vacío en este entorno); no se enviará",
				env.Name(), env.Value()))
			continue
		}
		secrets[env.Name()] = value
	}
	return secrets
}
