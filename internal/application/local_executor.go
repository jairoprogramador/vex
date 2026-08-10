package application

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	docPor "github.com/jairoprogramador/vex/internal/domain/docker/ports"
	docVos "github.com/jairoprogramador/vex/internal/domain/docker/vos"
	proPor "github.com/jairoprogramador/vex/internal/domain/project/ports"
	proVos "github.com/jairoprogramador/vex/internal/domain/project/vos"
	"github.com/jairoprogramador/vex/internal/infrastructure/project/mapper"
)

const (
	MessageProjectNotInitialized = "project not initialized. Please run 'vex init' first"

	// requestInputEnvVar es la env var que `vexd run` lee para obtener el
	// RequestInput JSON. La codificamos en base64 para evitar que las
	// comillas/saltos de línea del JSON rompan el shell que invoca docker.
	requestInputEnvVar = "VEX_REQUEST_INPUT"

	// stateConfigEnvVar es la env var que `vexd run` lee para obtener la
	// configuración de destino del estado. Se elige la env var y no
	// `--state-config <archivo>` porque el mecanismo ya existe y funciona para
	// el RequestInput, y evita montar un archivo más dentro del contenedor
	// (spec 23 §5.1).
	stateConfigEnvVar = "VEX_STATE_CONFIG"
)

type LocalExecutorService struct {
	projectRepository proPor.ProjectRepository
	commandExecutor   docPor.CommandExecutor
	imageService      docPor.ImageService
	containerService  docPor.ContainerService
}

func NewLocalExecutorService(
	projectRepository proPor.ProjectRepository,
	commandExecutor docPor.CommandExecutor,
	imageService docPor.ImageService,
	containerService docPor.ContainerService,
) *LocalExecutorService {
	return &LocalExecutorService{
		projectRepository: projectRepository,
		commandExecutor:   commandExecutor,
		imageService:      imageService,
		containerService:  containerService,
	}
}

func (s *LocalExecutorService) Run(ctx context.Context, command, environment string) error {
	exists, err := s.projectRepository.Exists()
	if err != nil {
		return err
	}
	if !exists {
		return errors.New(MessageProjectNotInitialized)
	}

	project, err := s.projectRepository.Load()
	if err != nil {
		return err
	}

	projectCwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("local executor: obtener directorio de trabajo: %w", err)
	}
	localVol, err := proVos.NewVolume(projectCwd, containerProjectPath)
	if err != nil {
		return fmt.Errorf("local executor: construir volumen %s: %w", containerProjectPath, err)
	}

	// El destino. vexHomeDir() crea el `~/.vex` del host antes de montarlo, y
	// desde la spec 16 esa línea es LOAD-BEARING y no una cortesía: el motor
	// hace `stat` sobre el destino al cablear y sale con exit code 2 si no
	// existe, no es un directorio o no es escribible. `type: local` no es un
	// no-op, y desde la spec 20 además tiene que poder crear ahí `keys/`.
	hostVexHome, err := vexHomeDir()
	if err != nil {
		return fmt.Errorf("local executor: resolver directorio vex del host: %w", err)
	}
	stateVol, err := proVos.NewVolume(hostVexHome, containerStatePath)
	if err != nil {
		return fmt.Errorf("local executor: construir volumen %s: %w", containerStatePath, err)
	}

	// Y el área de trabajo, que es la OTRA raíz del motor y por eso es otro
	// volumen. Persiste en el host para que la ventana de reutilización del clon
	// (spec 18) tenga dónde vivir, pero vive aparte del destino porque lo que
	// hay ahí es derivable y lo que hay en el destino no.
	hostVexWork, err := vexWorkDir()
	if err != nil {
		return fmt.Errorf("local executor: resolver área de trabajo del host: %w", err)
	}
	vexWorkVol, err := proVos.NewVolume(hostVexWork, containerVexWorkPath)
	if err != nil {
		return fmt.Errorf("local executor: construir volumen %s: %w", containerVexWorkPath, err)
	}

	// El staging del motor no puede caer dentro de ninguno de los dos. Se
	// comprueba antes de construir la imagen para que el aborto no cueste un
	// `docker build` (spec 23 §5.4).
	if err := ensureStagingOutsideMounts(project.Runtime().Env(),
		stateVol.Container(), vexWorkVol.Container()); err != nil {
		return fmt.Errorf("local executor: %w", err)
	}

	project.SetRuntime(project.Runtime().WithExtraVolume(localVol, stateVol, vexWorkVol))

	imageInfo := project.Runtime().Image()

	var imageToUse docVos.ImageName
	if !imageInfo.TagExplicit() {
		imageOptions, err := s.imageService.CreateOptions(project)
		if err != nil {
			return err
		}

		buildCommand, err := s.imageService.BuildCommand(imageOptions)
		if err != nil {
			return err
		}

		if _, err = s.commandExecutor.Execute(ctx, buildCommand); err != nil {
			return err
		}
		imageToUse = imageOptions.Image()
	} else {
		imageToUse, err = docVos.NewImageName(imageInfo.Image(), imageInfo.Tag())
		if err != nil {
			return err
		}
	}

	requestInput, err := mapper.ToRequestInput(project, command, environment)
	if err != nil {
		return fmt.Errorf("build request input: %w", err)
	}

	encoded, err := encodeRequestInput(requestInput)
	if err != nil {
		return fmt.Errorf("encode request input: %w", err)
	}

	// La env var con el RequestInput se inyecta en project.Runtime().Env(); el
	// containerService la traslada a -e VEX_REQUEST_INPUT=<base64> en docker run.
	envVar, err := proVos.NewEnvVar(requestInputEnvVar, encoded)
	if err != nil {
		return fmt.Errorf("build env var %s: %w", requestInputEnvVar, err)
	}

	// Y la configuración de destino, compuesta a partir del montaje que esta
	// misma función acaba de resolver. El motor no tiene destino por defecto: si
	// esta env var falta, no arranca.
	stateConfig, err := encodeStateConfig(stateVol.Container())
	if err != nil {
		return fmt.Errorf("local executor: componer configuración de destino: %w", err)
	}
	stateConfigVar, err := proVos.NewEnvVar(stateConfigEnvVar, stateConfig)
	if err != nil {
		return fmt.Errorf("build env var %s: %w", stateConfigEnvVar, err)
	}

	project.SetRuntime(project.Runtime().WithExtraEnv(envVar, stateConfigVar))

	// Las dos rutas que quedaron sueltas cuando la spec 16 retiró `--mode`. El
	// string se append al comando docker run tras la imagen, así que el proceso
	// efectivo dentro del contenedor es:
	//
	//	vexd run --project-path /appProject --vex-home /vexHome
	//
	// No son comodidad: sin `--project-path` el motor clona el proyecto en vez
	// de usar el árbol de trabajo montado, y sin `--vex-home` los clones y los
	// workdirs caen en el $HOME efímero del contenedor, con lo que la ventana de
	// reutilización del pipelinecode no reutiliza nada. Los dos síntomas son
	// «no funciona» sin que nada falle.
	//
	// `--vex-home` recibe la RAÍZ y no el montaje, porque el motor le añade el
	// `.vex`. Pasarle el montaje pondría los clones en `<montaje>/.vex/projects`,
	// o sea un `.vex` dentro del directorio de trabajo del host.
	entrypointArgs := fmt.Sprintf("--project-path %s --vex-home %s",
		localVol.Container(), containerVexRootPath)

	containerOptions, err := s.containerService.CreateOptions(project, entrypointArgs, imageToUse)
	if err != nil {
		return err
	}

	runCommand, err := s.containerService.BuildCommand(containerOptions)
	if err != nil {
		return err
	}

	_, err = s.commandExecutor.Execute(ctx, runCommand)
	return err
}

var _ Runner = (*LocalExecutorService)(nil)

// vexHomeDir es el DESTINO en el host: `~/.vex`. Ahí viven `state/`, `cache/`,
// `lineage/` y `keys/`, y ahí empuja el motor `objects/` y `events/`.
//
// Crear el directorio no es una cortesía: el motor comprueba el destino al
// cablear y sale con exit code 2 si no existe o no es escribible. Y desde la
// spec 20 lo que se acumula ahí ya no es sólo estado —está `keys/digest-v1.key`,
// 0600, del que se derivan los digests de cada parámetro—, así que tiene que
// caer en un sitio privado del usuario y no en una carpeta sincronizada.
func vexHomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	dir := filepath.Join(home, ".vex")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("create vex home %q: %w", dir, err)
	}
	return dir, nil
}

// vexWorkDir es el ÁREA DE TRABAJO en el host: los clones del proyecto y del
// pipelinecode, y los workdirs. Va bajo el directorio de caché del usuario
// —`~/Library/Caches/vex` en macOS, `~/.cache/vex` en Linux— y no bajo `~/.vex`,
// y la razón es la distinción que el motor hace entre sus dos raíces:
//
//	lo de aquí es DERIVABLE: borrarlo cuesta un clon
//	lo del destino es la VERDAD: ahí vive el ARN de un recurso que ya existe
//
// Mezclarlas en una carpeta funcionaría —el motor lo tolera— pero haría que
// «vaciar los clones» y «borrar el registro» se parecieran demasiado.
func vexWorkDir() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache dir: %w", err)
	}
	dir := filepath.Join(cache, "vex")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("create vex work dir %q: %w", dir, err)
	}
	return dir, nil
}

func encodeRequestInput(input mapper.RequestInputJSON) (string, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("marshal request input: %w", err)
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}
