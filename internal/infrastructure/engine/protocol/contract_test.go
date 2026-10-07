package protocol_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jairoprogramador/vex/internal/infrastructure/engine/protocol"
)

// Test de contrato: ejecuta un vexd real y decodifica sus respuestas de forma
// estricta. Si el motor añade o renombra un campo, este test falla en vez de
// fallar en producción. Se activa con VEXD_BIN=<ruta al binario>; el pipeline
// de ejemplo sale de VEXD_EJEMPLO o de <vexd>/../internal/ejecucion/testdata/ejemplo.

type description struct {
	VersionDelMotor      string
	VersionesDelLenguaje []string
	Operaciones          []string
}

func strictDecode(t *testing.T, raw json.RawMessage, out any) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	require.NoError(t, dec.Decode(out), "contrato roto con: %s", raw)
}

type engineRun struct {
	notifications []*protocol.Message
	response      *protocol.Message
}

func runEngine(t *testing.T, bin string, env []string, req protocol.Request) engineRun {
	t.Helper()
	var in bytes.Buffer
	require.NoError(t, protocol.NewWriter(&in).Write(req))

	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = &in
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	_ = cmd.Run() // el código de salida se interpreta por la respuesta

	var run engineRun
	reader := protocol.NewReader(&stdout)
	for {
		msg, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err, "stderr: %s", stderr.String())
		if msg.IsNotification() {
			run.notifications = append(run.notifications, msg)
		} else {
			run.response = msg
		}
	}
	require.NotNil(t, run.response, "sin respuesta; stderr: %s", stderr.String())
	return run
}

func gitRepo(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "-A"},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "inicial"},
	} {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
}

func copyDir(t *testing.T, from, to string) {
	t.Helper()
	out, err := exec.Command("cp", "-R", from+"/.", to).CombinedOutput()
	require.NoError(t, err, string(out))
}

func engineBinary(t *testing.T) string {
	t.Helper()
	bin := os.Getenv("VEXD_BIN")
	if bin == "" {
		t.Skip("VEXD_BIN no definido: test de contrato omitido")
	}
	return bin
}

func TestContract_Describir(t *testing.T) {
	bin := engineBinary(t)

	run := runEngine(t, bin, nil, protocol.NewRequest("1", "describir", map[string]string{}, nil))

	var got description
	require.NoError(t, protocol.DecodeResult(run.response, &got))
	strictDecode(t, run.response.Result, &description{})
	assert.Contains(t, got.VersionesDelLenguaje, protocol.LanguageVersion)
	assert.Contains(t, got.Operaciones, protocol.MethodIntentar)
	assert.Contains(t, got.Operaciones, protocol.MethodLogs)
}

func TestContract_OperacionDesconocida(t *testing.T) {
	bin := engineBinary(t)

	run := runEngine(t, bin, nil, protocol.NewRequest("1", "no_existe", map[string]string{}, nil))

	err := protocol.DecodeResult(run.response, &struct{}{})
	assert.ErrorIs(t, err, protocol.ErrOperacionDesconocida)
}

func TestContract_IntentarYLogs(t *testing.T) {
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

	store, space, material := filepath.Join(base, "almacen"), filepath.Join(base, "espacio"), filepath.Join(base, "material")
	require.NoError(t, os.MkdirAll(store, 0o755))
	env := []string{"VEX_ALMACEN=" + store, "VEX_ESPACIO=" + space, "VEX_MATERIAL=" + material}

	attempt := protocol.PeticionDeIntento{
		Version:           protocol.LanguageVersion,
		Ambiente:          "sand",
		Solicitante:       "contract-test",
		FuenteDelProyecto: project,
		FuenteDelPipeline: pipeline,
		HastaPaso:         "test",
		Metadatos:         protocol.Metadatos{ProjectId: "p1", ProjectName: "vex-demo"},
	}
	run := runEngine(t, bin, env, protocol.NewRequest("1", protocol.MethodIntentar, attempt, nil))

	var result protocol.Resultado
	require.NoError(t, protocol.DecodeResult(run.response, &result))
	strictDecode(t, run.response.Result, &protocol.Resultado{})
	assert.Equal(t, protocol.EstadoExitoso, result.Estado)
	require.NotEmpty(t, result.Detalle.Pasos)

	require.NotEmpty(t, run.notifications)
	events := make([]string, 0, len(run.notifications))
	for _, n := range run.notifications {
		strictDecode(t, n.Params, &protocol.Progreso{})
		progress, err := protocol.DecodeProgress(n)
		require.NoError(t, err)
		events = append(events, progress.Evento)
	}
	assert.Equal(t, protocol.EventoIntentoIniciado, events[0])
	assert.Contains(t, events, protocol.EventoPasoTerminado)

	logsReq := protocol.NewRequest("2", protocol.MethodLogs, protocol.PeticionDeLogs{
		Version: protocol.LanguageVersion,
		Intento: result.Intento,
	}, nil)
	logsRun := runEngine(t, bin, env, logsReq)

	var logs protocol.Logs
	require.NoError(t, protocol.DecodeResult(logsRun.response, &logs))
	strictDecode(t, logsRun.response.Result, &protocol.Logs{})
	require.NotEmpty(t, logs.Salidas)
	assert.Equal(t, result.Intento, logs.Intento)
	assert.True(t, strings.Contains(logs.Salidas[0].Paso, "test"))
}
