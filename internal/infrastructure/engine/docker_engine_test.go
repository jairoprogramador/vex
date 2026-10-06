package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jairoprogramador/vex/internal/application"
)

// TestHelperProcess no es un test: es el "docker" falso. El binario de test se
// re-ejecuta con GO_WANT_HELPER_PROCESS=1 y actúa según HELPER_SCENARIO.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	runFakeDocker(args)
	os.Exit(0)
}

func runFakeDocker(args []string) {
	dir := os.Getenv("HELPER_DIR")
	if args[0] == "kill" {
		_ = os.WriteFile(filepath.Join(dir, "killed"), []byte(args[1]), 0o644)
		return
	}
	_ = os.WriteFile(filepath.Join(dir, "args.json"), mustJSON(args), 0o644)

	stdin := bufio.NewReader(os.Stdin)
	first, _ := stdin.ReadString('\n')
	_ = os.WriteFile(filepath.Join(dir, "request.json"), []byte(first), 0o644)

	progress := func(evento, extra string) {
		fmt.Printf(`{"jsonrpc":"2.0","method":"progreso","params":{"evento":%q%s}}`+"\n", evento, extra)
	}

	switch os.Getenv("HELPER_SCENARIO") {
	case "success":
		progress("intento_iniciado", `,"intento":"i1"`)
		progress("paso_iniciado", `,"intento":"i1","paso":"test"`)
		progress("evento_futuro", ``)
		progress("comando_terminado", `,"intento":"i1","paso":"test","comando":"c1","estado":"exitoso"`)
		progress("paso_terminado", `,"intento":"i1","paso":"test","estado":"precargado"`)
		fmt.Println(`{"jsonrpc":"2.0","id":"1","result":{"Intento":"i1","Estado":"exitoso","Despliegue":"d1","Detalle":{"Tiempo":"1s","Pasos":[{"Nombre":"test","Estado":"precargado"}]}}}`)
	case "logs":
		fmt.Println(`{"jsonrpc":"2.0","id":"1","result":{"Intento":"i1","Ambiente":"sand","Salidas":[{"Paso":"test","Comando":"c1","Exitoso":false,"Texto":"boom","Instante":"2026-01-01T00:00:00Z"}]}}`)
	case "busy":
		fmt.Println(`{"jsonrpc":"2.0","id":"1","error":{"code":-32004,"message":"ocupado","data":{"tipo":"ambiente_ocupado","ambiente":"sand","intento":"i9"}}}`)
		os.Exit(1)
	case "internal":
		fmt.Fprintln(os.Stderr, "panic: causa real")
		fmt.Println(`{"jsonrpc":"2.0","id":"1","error":{"code":-32000,"message":"interno","data":{"tipo":"interno"}}}`)
		os.Exit(1)
	case "no_response":
		fmt.Fprintln(os.Stderr, "Unable to find image")
		os.Exit(125)
	case "garbled":
		fmt.Println("esto no es json")
		waitForKill(dir)
	case "cancel_ok":
		progress("intento_iniciado", `,"intento":"i1"`)
		for {
			line, err := stdin.ReadString('\n')
			if err != nil {
				os.Exit(1)
			}
			if strings.Contains(line, `"cancelar"`) {
				break
			}
		}
		fmt.Println(`{"jsonrpc":"2.0","id":"1","result":{"Intento":"i1","Estado":"cancelado","Detalle":{"Tiempo":"1s","Pasos":[]}}}`)
		os.Exit(130)
	case "cancel_ignored":
		progress("intento_iniciado", `,"intento":"i1"`)
		waitForKill(dir)
	}
}

// waitForKill simula un contenedor que solo muere con "docker kill".
func waitForKill(dir string) {
	for {
		if _, err := os.Stat(filepath.Join(dir, "killed")); err == nil {
			os.Exit(137)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

type fakeDocker struct {
	engine *DockerEngine
	dir    string
}

func newFakeDocker(t *testing.T, scenario string) fakeDocker {
	t.Helper()
	dir := t.TempDir()
	engine := NewDockerEngine()
	engine.newName = func() string { return "vex-test" }
	engine.cancelGrace = 300 * time.Millisecond
	engine.newCommand = func(args ...string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], append([]string{"-test.run=TestHelperProcess", "--"}, args...)...)
		cmd.Env = append(os.Environ(),
			"GO_WANT_HELPER_PROCESS=1", "HELPER_SCENARIO="+scenario, "HELPER_DIR="+dir)
		return cmd
	}
	return fakeDocker{engine: engine, dir: dir}
}

func (f fakeDocker) readFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.dir, name))
	require.NoError(t, err)
	return string(b)
}

var testSpec = application.ContainerSpec{
	Image: "vex-runtime:latest",
	Env:   []application.EnvVar{{Name: "VEX_ALMACEN", Value: "/vex/almacen"}},
	Mounts: []application.Mount{
		{Source: "/host/proyecto", Target: "/proyecto", ReadOnly: true},
		{Source: "/host/alma,cen", Target: "/vex/almacen"},
	},
}

var testAttempt = application.AttemptRequest{
	Environment:    "sand",
	Requester:      "jailux",
	ProjectSource:  "/proyecto",
	PipelineSource: "/pipeline",
	UntilStep:      "test",
	Project:        application.ProjectMetadata{ID: "p1", Name: "app", Organization: "org", Team: "team"},
	Secrets:        map[string]string{"TOKEN": "s3cr3t"},
}

func TestDockerEngine_Attempt_Exitoso(t *testing.T) {
	fake := newFakeDocker(t, "success")
	var events []application.EngineEvent

	result, err := fake.engine.Attempt(context.Background(), testSpec, testAttempt,
		func(e application.EngineEvent) { events = append(events, e) })

	require.NoError(t, err)
	assert.Equal(t, application.AttemptResult{
		AttemptID: "i1", Status: application.AttemptSucceeded, DeploymentID: "d1", Duration: "1s",
		Steps: []application.StepResult{{Name: "test", Status: application.StepReused}},
	}, result)
	assert.Equal(t, []application.EngineEvent{
		{Kind: application.AttemptStarted, AttemptID: "i1"},
		{Kind: application.StepStarted, AttemptID: "i1", Step: "test"},
		{Kind: application.CommandFinished, AttemptID: "i1", Step: "test", Command: "c1", Succeeded: true},
		{Kind: application.StepFinished, AttemptID: "i1", Step: "test", StepStatus: application.StepReused},
	}, events, "el evento desconocido se ignora")
}

func TestDockerEngine_Attempt_ArmaElComandoDockerSinShell(t *testing.T) {
	fake := newFakeDocker(t, "success")

	_, err := fake.engine.Attempt(context.Background(), testSpec, testAttempt, nil)
	require.NoError(t, err)

	var args []string
	require.NoError(t, json.Unmarshal([]byte(fake.readFile(t, "args.json")), &args))
	assert.Equal(t, []string{
		"run", "--rm", "-i", "--name", "vex-test",
		"-e", "VEX_ALMACEN=/vex/almacen",
		"--mount", "type=bind,source=/host/proyecto,target=/proyecto,readonly",
		"--mount", `type=bind,"source=/host/alma,cen",target=/vex/almacen`,
		"vex-runtime:latest",
	}, args)
}

func TestDockerEngine_Attempt_EnviaLaPeticionPorStdinYLosSecretosFueraDeArgv(t *testing.T) {
	fake := newFakeDocker(t, "success")

	_, err := fake.engine.Attempt(context.Background(), testSpec, testAttempt, nil)
	require.NoError(t, err)

	assert.NotContains(t, fake.readFile(t, "args.json"), "s3cr3t")

	var sent struct {
		JSONRPC string
		Method  string
		Params  map[string]any
		Entorno map[string]string
	}
	require.NoError(t, json.Unmarshal([]byte(fake.readFile(t, "request.json")), &sent))
	assert.Equal(t, "intentar", sent.Method)
	assert.Equal(t, "1", sent.Params["Version"])
	assert.Equal(t, "sand", sent.Params["Ambiente"])
	assert.Equal(t, "/proyecto", sent.Params["FuenteDelProyecto"])
	assert.Equal(t, "test", sent.Params["HastaPaso"])
	assert.Equal(t, map[string]string{"TOKEN": "s3cr3t"}, sent.Entorno)
}

func TestDockerEngine_Attempt_ErroresDelMotor(t *testing.T) {
	tests := []struct {
		name     string
		scenario string
		kind     application.EngineErrorKind
		check    func(t *testing.T, e *application.EngineError)
	}{
		{"ambiente ocupado", "busy", application.EngineEnvironmentBusy, func(t *testing.T, e *application.EngineError) {
			assert.Equal(t, "sand", e.Environment)
			assert.Equal(t, "i9", e.AttemptID)
			assert.Empty(t, e.Stderr)
		}},
		{"interno adjunta stderr", "internal", application.EngineInternal, func(t *testing.T, e *application.EngineError) {
			assert.Equal(t, "panic: causa real", e.Stderr)
		}},
		{"sin respuesta explica con stderr", "no_response", application.EngineDidNotRespond, func(t *testing.T, e *application.EngineError) {
			assert.Equal(t, "Unable to find image", e.Stderr)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeDocker(t, tt.scenario)

			_, err := fake.engine.Attempt(context.Background(), testSpec, testAttempt, nil)

			var engineErr *application.EngineError
			require.ErrorAs(t, err, &engineErr)
			assert.Equal(t, tt.kind, engineErr.Kind)
			tt.check(t, engineErr)
		})
	}
}

func TestDockerEngine_Attempt_LineaIlegibleMataElContenedor(t *testing.T) {
	fake := newFakeDocker(t, "garbled")

	_, err := fake.engine.Attempt(context.Background(), testSpec, testAttempt, nil)

	assert.ErrorContains(t, err, "leer respuesta del motor")
	assert.Equal(t, "vex-test", fake.readFile(t, "killed"))
}

func TestDockerEngine_Attempt_CancelacionOrdenadaDevuelveResultado(t *testing.T) {
	fake := newFakeDocker(t, "cancel_ok")
	ctx, cancel := context.WithCancel(context.Background())

	result, err := fake.engine.Attempt(ctx, testSpec, testAttempt, func(e application.EngineEvent) {
		if e.Kind == application.AttemptStarted {
			cancel()
		}
	})

	require.NoError(t, err, "un intento cancelado es un resultado, no un error")
	assert.Equal(t, application.AttemptCanceled, result.Status)
	assert.NoFileExists(t, filepath.Join(fake.dir, "killed"))
}

func TestDockerEngine_Attempt_SiElMotorIgnoraCancelarSeMataElContenedor(t *testing.T) {
	fake := newFakeDocker(t, "cancel_ignored")
	ctx, cancel := context.WithCancel(context.Background())

	_, err := fake.engine.Attempt(ctx, testSpec, testAttempt, func(e application.EngineEvent) {
		if e.Kind == application.AttemptStarted {
			cancel()
		}
	})

	var engineErr *application.EngineError
	require.ErrorAs(t, err, &engineErr)
	assert.Equal(t, application.EngineDidNotRespond, engineErr.Kind)
	assert.Equal(t, "vex-test", fake.readFile(t, "killed"))
}

func TestDockerEngine_Attempt_ContextoYaCancelado(t *testing.T) {
	fake := newFakeDocker(t, "success")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := fake.engine.Attempt(ctx, testSpec, testAttempt, nil)

	assert.ErrorIs(t, err, context.Canceled)
	assert.NoFileExists(t, filepath.Join(fake.dir, "args.json"), "no debe lanzar el contenedor")
}

func TestDockerEngine_Logs(t *testing.T) {
	fake := newFakeDocker(t, "logs")

	outputs, err := fake.engine.Logs(context.Background(), testSpec,
		application.LogsRequest{AttemptID: "i1", OnlyFailed: true})

	require.NoError(t, err)
	assert.Equal(t, []application.CommandOutput{{Step: "test", Command: "c1", Succeeded: false, Text: "boom"}}, outputs)

	var sent struct {
		Method string
		Params map[string]string
	}
	require.NoError(t, json.Unmarshal([]byte(fake.readFile(t, "request.json")), &sent))
	assert.Equal(t, "logs", sent.Method)
	assert.Equal(t, map[string]string{"Version": "1", "Intento": "i1", "Resultado": "fallido"}, sent.Params)
}
