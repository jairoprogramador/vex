package application

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	comVos "github.com/jairoprogramador/vex/internal/domain/common/vos"
	dockerServices "github.com/jairoprogramador/vex/internal/domain/docker/services"
	"github.com/jairoprogramador/vex/internal/domain/project/aggregates"
	proVos "github.com/jairoprogramador/vex/internal/domain/project/vos"
	"github.com/jairoprogramador/vex/internal/infrastructure/project/mapper"
)

// recordingExecutor se queda con las líneas de comando en vez de ejecutarlas.
// Es todo lo que hace falta para las aserciones de la spec 23: lo que esta spec
// cambia es exactamente lo que la CLI le DICE al motor.
type recordingExecutor struct{ commands []string }

func (r *recordingExecutor) Execute(_ context.Context, command string) (string, error) {
	r.commands = append(r.commands, command)
	return "", nil
}

// newLocalProject construye un proyecto con la imagen y el tag explícitos, que
// es lo que evita el `docker build` y deja una sola línea de comando que mirar.
func newLocalProject(t *testing.T, env ...proVos.EnvVar) *aggregates.Project {
	t.Helper()

	id, err := proVos.NewProjectID("seed-id")
	require.NoError(t, err)
	data, err := proVos.NewProjectData("demo", "vex", "shikigami", "desc",
		"https://github.com/local/repo", "main")
	require.NoError(t, err)
	pipeline, err := comVos.NewPipeline("https://github.com/local/pipe", "main")
	require.NoError(t, err)
	image, err := comVos.NewImage("local/runtime:v1")
	require.NoError(t, err)

	runtime := proVos.NewRuntime(proVos.WithImage(image), proVos.WithEnv(env))
	project, err := aggregates.NewProject(id, data, pipeline, runtime)
	require.NoError(t, err)
	return project
}

func newLocalExecutor(t *testing.T, project *aggregates.Project) (*LocalExecutorService, *recordingExecutor) {
	t.Helper()

	// Los dos directorios que la CLI monta se crean bajo un HOME temporal: la
	// ejecución del test no puede tocar el del usuario.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CACHE_HOME", home+"/.cache")

	executor := &recordingExecutor{}
	service := NewLocalExecutorService(
		&stubProjectRepo{loaded: project, existsBool: true},
		executor,
		dockerServices.NewImageBuilder(),
		dockerServices.NewContainerBuilder(),
	)
	return service, executor
}

// envValueOf extrae el valor de un `-e NOMBRE=valor` de la línea de docker run.
func envValueOf(t *testing.T, command, name string) string {
	t.Helper()

	marker := " -e " + name + "="
	idx := strings.Index(command, marker)
	require.NotEqual(t, -1, idx, "la línea no lleva %s: %s", name, command)

	rest := command[idx+len(marker):]
	if end := strings.Index(rest, " "); end != -1 {
		rest = rest[:end]
	}
	return rest
}

// hostSourceOf devuelve el `source=` del montaje cuyo `target=` es el dado.
func hostSourceOf(t *testing.T, command, target string) string {
	t.Helper()

	for _, campo := range strings.Fields(command) {
		if !strings.HasPrefix(campo, "type=bind,source=") {
			continue
		}
		source, tgt, ok := strings.Cut(strings.TrimPrefix(campo, "type=bind,source="), ",target=")
		if ok && tgt == target {
			return source
		}
	}
	t.Fatalf("no hay montaje con target %s en: %s", target, command)
	return ""
}

func TestLocalExecutor_ElEntrypointRecibeLasDosRutasYNoRecibeMode(t *testing.T) {
	service, executor := newLocalExecutor(t, newLocalProject(t))

	require.NoError(t, service.Run(context.Background(), "deploy", "sand"))
	require.Len(t, executor.commands, 1, "con tag explícito no hay build: sólo el run")

	command := executor.commands[0]

	assert.NotContains(t, command, "--mode",
		"la spec 16 retiró el enum: pasarlo hoy es exit code 2")
	assert.Contains(t, command, "--project-path /appProject",
		"sin esta bandera el motor CLONA el proyecto y despliega el HEAD del remoto,"+
			" no el árbol de trabajo montado")
	assert.Contains(t, command, "--vex-home /vexHome",
		"sin esta bandera los clones caen en el $HOME efímero del contenedor")
	assert.NotContains(t, command, "--vex-home /vexHome/.vex",
		"`--vex-home` es la RAÍZ: el motor le añade el .vex, y pasarle el montaje"+
			" metería un .vex dentro del directorio de trabajo del host")
}

func TestLocalExecutor_LasDosRaicesDelMotorSonDosVolumenes(t *testing.T) {
	service, executor := newLocalExecutor(t, newLocalProject(t))

	require.NoError(t, service.Run(context.Background(), "deploy", "sand"))
	command := executor.commands[0]

	assert.Contains(t, command, "target=/vexState",
		"el destino: ~/.vex del host, donde viven state/, cache/, lineage/ y keys/")
	assert.Contains(t, command, "target=/vexHome/.vex",
		"el área de trabajo: los clones, que son derivables y van aparte")
	assert.Contains(t, command, "target=/appProject")

	// Y los dos montajes son DISTINTOS en el host: mezclarlos juntaría lo
	// desechable con la verdad, que es la distinción que el motor hace entre
	// sus dos raíces.
	state := hostSourceOf(t, command, "/vexState")
	work := hostSourceOf(t, command, "/vexHome/.vex")
	assert.NotEqual(t, state, work)
	assert.False(t, strings.HasPrefix(work, state+"/"),
		"el área de trabajo no puede colgar del destino")
}

func TestLocalExecutor_LaConfiguracionDeDestinoApuntaAlVolumenMontado(t *testing.T) {
	service, executor := newLocalExecutor(t, newLocalProject(t))

	require.NoError(t, service.Run(context.Background(), "deploy", "sand"))

	raw, err := base64.StdEncoding.DecodeString(
		envValueOf(t, executor.commands[0], stateConfigEnvVar))
	require.NoError(t, err)

	var cfg stateConfigJSON
	require.NoError(t, json.Unmarshal(raw, &cfg))

	assert.Equal(t, "local", cfg.Type)
	assert.Equal(t, "/vexState", cfg.Local.Path,
		"el destino es el volumen con el ~/.vex del host: es lo que hace que el"+
			" state/ escrito antes de la spec 16 se siga leyendo sin migrar nada")
}

func TestLocalExecutor_ElRequestInputSaleConLaVersionQueElMotorAcepta(t *testing.T) {
	service, executor := newLocalExecutor(t, newLocalProject(t))

	require.NoError(t, service.Run(context.Background(), "deploy", "sand"))

	raw, err := base64.StdEncoding.DecodeString(
		envValueOf(t, executor.commands[0], requestInputEnvVar))
	require.NoError(t, err)

	var input mapper.RequestInputJSON
	require.NoError(t, json.Unmarshal(raw, &input))

	assert.Equal(t, 2, input.SchemaVersion)
	assert.Equal(t, "deploy", input.Execution.Step)
	assert.Equal(t, "sand", input.Execution.Environment)
}

func TestLocalExecutor_AbortaSiElStagingCaeDentroDeUnMontaje(t *testing.T) {
	// Un XDG_STATE_HOME declarado en los `envs` del proyecto que apunte dentro
	// de un montaje. El motor lo descartaría en silencio y reubicaría el área;
	// desde aquí se prefiere decirlo, porque quien lo declaró quiso otra cosa.
	// Y contra el DESTINO esta comprobación es la única: la guarda del motor
	// mira su área de trabajo, que ya no es el mismo volumen.
	casos := map[string]string{
		"dentro del destino":         "/vexState/estado",
		"dentro del área de trabajo": "/vexHome/.vex/estado",
	}

	for nombre, ruta := range casos {
		t.Run(nombre, func(t *testing.T) {
			xdg, err := proVos.NewEnvVar("XDG_STATE_HOME", ruta)
			require.NoError(t, err)

			service, executor := newLocalExecutor(t, newLocalProject(t, xdg))

			err = service.Run(context.Background(), "deploy", "sand")

			require.Error(t, err)
			assert.Contains(t, err.Error(), "objects/, events/ y ack/")
			assert.Empty(t, executor.commands,
				"el aborto es antes del build: no debe costar un docker build")
		})
	}
}
