package protocol_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jairoprogramador/vex/internal/infrastructure/engine/protocol"
)

// Contrato de TODAS las operaciones contra un vexd real (VEXD_BIN). Una sola prueba recorre un escenario
// completo —un despliegue, un lanzamiento, una reserva, un fallo, un rollback— porque cada operación necesita el
// estado que dejaron las anteriores. Toda respuesta se decodifica de forma estricta.

func head(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}

func commitAll(t *testing.T, dir, message string) {
	t.Helper()
	for _, args := range [][]string{
		{"add", "-A"},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", message},
	} {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
}

type scenario struct {
	t        *testing.T
	bin      string
	env      []string
	project  string
	pipeline string
}

func (s scenario) call(method string, params any) engineRun {
	return runEngine(s.t, s.bin, s.env, protocol.NewRequest("1", method, params, nil))
}

// ok decodifica el result de forma estricta y falla si la respuesta fue un error.
func (s scenario) ok(run engineRun, out any) {
	s.t.Helper()
	require.NoError(s.t, protocol.DecodeResult(run.response, out))
	strictDecode(s.t, run.response.Result, out)
}

// fails devuelve el error del motor de una respuesta que tiene que ser un error.
func (s scenario) fails(run engineRun) *protocol.EngineError {
	s.t.Helper()
	var engineErr *protocol.EngineError
	err := protocol.DecodeResult(run.response, &struct{}{})
	require.ErrorAs(s.t, err, &engineErr)
	return engineErr
}

func (s scenario) attempt(environment, untilStep string) protocol.PeticionDeIntento {
	return protocol.PeticionDeIntento{
		Version: protocol.LanguageVersion, Ambiente: environment, Solicitante: "contract-test", HastaPaso: untilStep,
		FuenteDelProyecto: s.project, CommitDelProyecto: head(s.t, s.project),
		FuenteDelPipeline: s.pipeline, CommitDelPipeline: head(s.t, s.pipeline),
		Metadatos: protocol.Metadatos{ProjectId: "p1", ProjectName: "vex-demo"},
	}
}

func newScenario(t *testing.T) scenario {
	t.Helper()
	bin := engineBinary(t)
	example := os.Getenv("VEXD_EJEMPLO")
	if example == "" {
		example = filepath.Join(filepath.Dir(bin), "internal", "ejecucion", "testdata", "ejemplo")
	}
	require.DirExists(t, example)

	base := t.TempDir()
	project, pipeline := filepath.Join(base, "proyecto"), filepath.Join(base, "pipeline")
	require.NoError(t, os.MkdirAll(project, 0o755))
	require.NoError(t, os.MkdirAll(pipeline, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(project, "README.md"), []byte("mi app\n"), 0o644))
	copyDir(t, example, pipeline)
	gitRepo(t, project)
	gitRepo(t, pipeline)

	store := filepath.Join(base, "almacen")
	require.NoError(t, os.MkdirAll(store, 0o755))
	return scenario{
		t: t, bin: bin, project: project, pipeline: pipeline,
		env: []string{
			"VEX_ALMACEN=" + store, "VEX_ESPACIO=" + filepath.Join(base, "espacio"),
			"VEX_MATERIAL=" + filepath.Join(base, "material"),
		},
	}
}

func TestContract_TodasLasOperaciones(t *testing.T) {
	s := newScenario(t)

	t.Run("describir lista las 16 operaciones", func(t *testing.T) {
		var got description
		s.ok(s.call("describir", map[string]string{}), &got)
		assert.ElementsMatch(t, []string{
			"abandonar", "ambientes", "describir", "despliegues", "diagnosticar", "intentar", "intento", "intentos",
			"lanzamientos", "lanzar", "liberar", "logs", "pasos", "reservar", "rollback", "simular",
		}, got.Operaciones)
	})

	// Un despliegue completo: todos los pasos, con commits.
	var first protocol.Resultado
	t.Run("intentar hasta el último paso crea un despliegue", func(t *testing.T) {
		s.ok(s.call(protocol.MethodIntentar, s.attempt("sand", "")), &first)
		require.Equal(t, protocol.EstadoExitoso, first.Estado)
		require.NotEmpty(t, first.Despliegue)
	})

	t.Run("intentos trae el instante y la causa", func(t *testing.T) {
		var list []protocol.ResumenDeIntento
		s.ok(s.call(protocol.MethodIntentos, protocol.PeticionPorAmbiente{Version: "1", Ambiente: "sand"}), &list)
		require.Len(t, list, 1)
		assert.Equal(t, first.Intento, list[0].Id)
		assert.Equal(t, protocol.EstadoExitoso, list[0].Estado)
		assert.False(t, list[0].Instante.IsZero(), "intentos ya trae Instante")
		assert.Empty(t, list[0].Causa)
	})

	t.Run("intento trae los registros de cada paso", func(t *testing.T) {
		var got protocol.IntentoDeHistorial
		s.ok(s.call(protocol.MethodIntento, protocol.PeticionPorIntento{Version: "1", Intento: first.Intento}), &got)
		assert.Equal(t, first.Intento, got.Id)
		assert.Equal(t, protocol.EstadoExitoso, got.Estado)
		assert.Len(t, got.Apertura.Pasos, 5)
		assert.NotEmpty(t, got.Registros)
		assert.False(t, got.Abandonado)
	})

	t.Run("despliegues y lanzamientos: el motor lanza solo si el ambiente no está reservado", func(t *testing.T) {
		var deployments []protocol.Despliegue
		s.ok(s.call(protocol.MethodDespliegues, protocol.PeticionPorAmbiente{Version: "1", Ambiente: "sand"}), &deployments)
		require.Len(t, deployments, 1)
		assert.Equal(t, first.Despliegue, deployments[0].Id)
		assert.Equal(t, first.Intento, deployments[0].Intento)

		var releases []protocol.Lanzamiento
		s.ok(s.call(protocol.MethodLanzamientos, protocol.PeticionPorAmbiente{Version: "1", Ambiente: "sand"}), &releases)
		require.Len(t, releases, 1, "lanzamiento automático")
		assert.Equal(t, first.Despliegue, releases[0].Despliegue)
	})

	t.Run("lanzar devuelve el id del lanzamiento y aparece en la lista", func(t *testing.T) {
		var released protocol.Lanzamiento
		s.ok(s.call(protocol.MethodLanzar, protocol.PeticionDeLanzar{
			Version: "1", Ambiente: "sand", Despliegue: first.Despliegue, Nombre: "v1",
		}), &released)
		assert.NotEmpty(t, released.Id)
		assert.Equal(t, "v1", released.Nombre)

		var releases []protocol.Lanzamiento
		s.ok(s.call(protocol.MethodLanzamientos, protocol.PeticionPorAmbiente{Version: "1", Ambiente: "sand"}), &releases)
		require.Len(t, releases, 2)
		assert.Equal(t, released.Id, releases[1].Id, "del más antiguo al más reciente")
	})

	t.Run("lanzar un despliegue de otro ambiente es rechazado", func(t *testing.T) {
		err := s.fails(s.call(protocol.MethodLanzar, protocol.PeticionDeLanzar{
			Version: "1", Ambiente: "prod", Despliegue: first.Despliegue,
		}))
		assert.ErrorIs(t, err, protocol.ErrRechazado)
	})

	t.Run("ambientes y pasos describen el pipeline; reservar y liberar cambian Reservado", func(t *testing.T) {
		catalog := protocol.PeticionDeCatalogo{Version: "1", FuenteDelPipeline: s.pipeline}
		reserved := func() map[string]bool {
			var environments []protocol.Ambiente
			s.ok(s.call(protocol.MethodAmbientes, catalog), &environments)
			result := map[string]bool{}
			for _, e := range environments {
				result[e.Valor] = e.Reservado
			}
			return result
		}
		assert.Equal(t, map[string]bool{"sand": false, "stag": false, "prod": false}, reserved())

		var empty struct{}
		s.ok(s.call(protocol.MethodReservar, protocol.PeticionPorAmbiente{Version: "1", Ambiente: "prod"}), &empty)
		assert.True(t, reserved()["prod"])
		s.ok(s.call(protocol.MethodLiberar, protocol.PeticionPorAmbiente{Version: "1", Ambiente: "prod"}), &empty)
		assert.False(t, reserved()["prod"])

		var steps []protocol.PasoDelPipeline
		s.ok(s.call(protocol.MethodPasos, catalog), &steps)
		require.Len(t, steps, 5)
		assert.Equal(t, protocol.PasoDelPipeline{Nombre: "test", Orden: 1}, steps[0])
		assert.Equal(t, "deploy", steps[4].Nombre)
	})

	t.Run("simular valida sin ejecutar", func(t *testing.T) {
		var result protocol.ResultadoDeSimulacion
		s.ok(s.call(protocol.MethodSimular, protocol.PeticionDeSimulacion{
			Version: "1", Ambiente: "sand", Solicitante: "contract-test", HastaPaso: "deploy",
			Fuente: s.pipeline, Commit: head(t, s.pipeline),
		}), &result)
		assert.Equal(t, protocol.EstadoExitoso, result.Estado)
		assert.Nil(t, result.Causa)
	})

	t.Run("un ambiente o un paso inexistente dice el campo y el valor", func(t *testing.T) {
		sim := func(environment, step string) protocol.PeticionDeSimulacion {
			return protocol.PeticionDeSimulacion{
				Version: "1", Ambiente: environment, Solicitante: "contract-test", HastaPaso: step,
				Fuente: s.pipeline, Commit: head(t, s.pipeline),
			}
		}
		cases := []struct {
			name, field, value string
			run                engineRun
		}{
			{"simular ambiente", "Ambiente", "nope", s.call(protocol.MethodSimular, sim("nope", "deploy"))},
			{"simular paso", "HastaPaso", "nope", s.call(protocol.MethodSimular, sim("sand", "nope"))},
			{"intentar ambiente", "Ambiente", "nope", s.call(protocol.MethodIntentar, s.attempt("nope", "test"))},
			{"intentar paso", "HastaPaso", "nope", s.call(protocol.MethodIntentar, s.attempt("sand", "nope"))},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				err := s.fails(c.run)
				assert.ErrorIs(t, err, protocol.ErrParametrosInvalidos)
				assert.Equal(t, c.field, err.Data.Campo)
				assert.Equal(t, c.value, err.Data.Valor)
			})
		}
	})

	t.Run("un parámetro desconocido o de otro tipo también dice el campo", func(t *testing.T) {
		unknown := s.fails(s.call(protocol.MethodIntentos, map[string]any{"Version": "1", "Ambiente": "sand", "Otro": 1}))
		assert.Equal(t, "Otro", unknown.Data.Campo)
		assert.Empty(t, unknown.Data.Valor, "en un campo desconocido, valor queda vacío")

		wrongType := s.fails(s.call(protocol.MethodIntentos, map[string]any{"Version": "1", "Ambiente": 5}))
		assert.Equal(t, "Ambiente", wrongType.Data.Campo)
		assert.Equal(t, "number", wrongType.Data.Valor, "en un tipo equivocado, trae el tipo que llegó")
	})

	t.Run("diagnosticar sin referencia, y rechaza intento y lanzamiento a la vez", func(t *testing.T) {
		var answer protocol.RespuestaDeDiagnostico
		s.ok(s.call(protocol.MethodDiagnosticar, protocol.PeticionDeDiagnostico{Version: "1", Intento: first.Intento}), &answer)
		assert.Equal(t, protocol.SinReferencia, answer.SinDiagnostico)

		err := s.fails(s.call(protocol.MethodDiagnosticar, protocol.PeticionDeDiagnostico{
			Version: "1", Intento: first.Intento, Lanzamiento: "x",
		}))
		assert.Equal(t, "Lanzamiento", err.Data.Campo)
	})

	t.Run("un fallo con un cambio detrás se diagnostica", func(t *testing.T) {
		// Cambia las instrucciones del primer paso para que falle: hay un despliegue anterior con el que comparar.
		file := filepath.Join(s.pipeline, "steps", "01-test", "commands.yaml")
		data, err := os.ReadFile(file)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(file, []byte(strings.Replace(string(data), "cmd: cat plantilla.txt", "cmd: exit 3", 1)), 0o644))
		commitAll(t, s.pipeline, "rompe el primer paso")

		var failed protocol.Resultado
		s.ok(s.call(protocol.MethodIntentar, s.attempt("sand", "test")), &failed)
		require.Equal(t, protocol.EstadoFallido, failed.Estado)

		var answer protocol.RespuestaDeDiagnostico
		s.ok(s.call(protocol.MethodDiagnosticar, protocol.PeticionDeDiagnostico{Version: "1", Ambiente: "sand"}), &answer)
		require.Empty(t, answer.SinDiagnostico)
		require.NotNil(t, answer.IntentoFallido)
		assert.Equal(t, failed.Intento, answer.IntentoFallido.Id)
		require.NotNil(t, answer.IntentoExitoso)
		assert.Equal(t, first.Intento, answer.IntentoExitoso.Id)
		require.NotNil(t, answer.Sustento)
		require.NotNil(t, answer.Sustento.Instrucciones, "cambió lo que hace el paso test")
		assert.Contains(t, answer.Sustento.Instrucciones.Pasos, "test")
	})

	t.Run("rollback vuelve a los commits de un despliegue anterior", func(t *testing.T) {
		var result protocol.Resultado
		run := s.call(protocol.MethodRollback, protocol.PeticionDeRollback{
			Version: "1", Despliegue: first.Despliegue, Solicitante: "contract-test",
		})
		s.ok(run, &result)
		assert.Equal(t, protocol.EstadoExitoso, result.Estado)
		assert.NotEmpty(t, result.Despliegue)
		assert.NotEqual(t, first.Despliegue, result.Despliegue)
		assert.NotEmpty(t, run.notifications, "el rollback emite progreso como un intento")

		var deployments []protocol.Despliegue
		s.ok(s.call(protocol.MethodDespliegues, protocol.PeticionPorAmbiente{Version: "1", Ambiente: "sand"}), &deployments)
		require.Len(t, deployments, 2)
		assert.Equal(t, first.Despliegue, deployments[1].Padre, "el rollback cuelga del despliegue al que vuelve")
	})

	t.Run("abandonar un intento cerrado, y consultar uno que no existe", func(t *testing.T) {
		err := s.fails(s.call(protocol.MethodAbandonar, protocol.PeticionPorIntento{Version: "1", Intento: first.Intento}))
		assert.ErrorIs(t, err, protocol.ErrRechazado)

		missing := s.fails(s.call(protocol.MethodIntento, protocol.PeticionPorIntento{Version: "1", Intento: "no-existe"}))
		assert.ErrorIs(t, missing, protocol.ErrNoExiste)
		assert.False(t, errors.Is(missing, protocol.ErrRechazado))
	})
}
