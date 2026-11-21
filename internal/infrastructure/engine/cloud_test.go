package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jairoprogramador/vex/internal/application"
	"github.com/jairoprogramador/vex/internal/infrastructure/engine/protocol"
)

// Lo que este adaptador hace para las credenciales de nube del portal: el contexto de ejecución, las variables
// visibles y, sobre todo, por dónde NO viaja el token.

func sentParams(t *testing.T, fake fakeDocker) map[string]any {
	t.Helper()
	var sent struct{ Params map[string]any }
	require.NoError(t, json.Unmarshal([]byte(fake.readFile(t, "request.json")), &sent))
	return sent.Params
}

func TestContexto_SoloViajaSiTieneValor(t *testing.T) {
	t.Run("intentar con contexto", func(t *testing.T) {
		fake := newFakeDocker(t, "success")
		req := testAttempt
		req.Context = "azure/v1:3f9a1c0d2b7e4a65"

		_, err := fake.engine.Attempt(context.Background(), testSpec, req, nil)

		require.NoError(t, err)
		assert.Equal(t, "azure/v1:3f9a1c0d2b7e4a65", sentParams(t, fake)["Contexto"])
	})
	t.Run("intentar sin contexto: un motor anterior no ve ningún campo nuevo", func(t *testing.T) {
		fake := newFakeDocker(t, "success")

		_, err := fake.engine.Attempt(context.Background(), testSpec, testAttempt, nil)

		require.NoError(t, err)
		assert.NotContains(t, sentParams(t, fake), "Contexto")
	})
	t.Run("rollback con y sin contexto", func(t *testing.T) {
		fake := newFakeDocker(t, "success")
		_, err := fake.engine.Rollback(context.Background(), testSpec,
			application.RollbackRequest{DeploymentID: "d1", Context: "azure/v1:abc"}, nil)
		require.NoError(t, err)
		assert.Equal(t, "azure/v1:abc", sentParams(t, fake)["Contexto"])

		fake = newFakeDocker(t, "success")
		_, err = fake.engine.Rollback(context.Background(), testSpec, application.RollbackRequest{DeploymentID: "d1"}, nil)
		require.NoError(t, err)
		assert.NotContains(t, sentParams(t, fake), "Contexto")
	})
	t.Run("simular nunca lo lleva", func(t *testing.T) {
		data, err := json.Marshal(toCheckParams(application.CheckRequest{Environment: "sand", UntilStep: "test"}))
		require.NoError(t, err)
		assert.NotContains(t, string(data), "Contexto")
	})
}

func TestContexto_UnMotorAnteriorLoRechazaYNoSeEjecutaNada(t *testing.T) {
	const unknownField = `{"jsonrpc":"2.0","id":"1","error":{"code":-32602,"message":"campo desconocido","data":{"tipo":"parametros_invalidos","campo":"Contexto"}}}`
	req := testAttempt
	req.Context = "azure/v1:abc"

	t.Run("intentar", func(t *testing.T) {
		fake := newFakeDockerReplying(t, unknownField)

		_, err := fake.engine.Attempt(context.Background(), testSpec, req, nil)

		var engineErr *application.EngineError
		require.ErrorAs(t, err, &engineErr)
		assert.Equal(t, application.EngineContextUnsupported, engineErr.Kind)
	})
	t.Run("rollback", func(t *testing.T) {
		fake := newFakeDockerReplying(t, unknownField)

		_, err := fake.engine.Rollback(context.Background(), testSpec,
			application.RollbackRequest{DeploymentID: "d1", Context: "azure/v1:abc"}, nil)

		var engineErr *application.EngineError
		require.ErrorAs(t, err, &engineErr)
		assert.Equal(t, application.EngineContextUnsupported, engineErr.Kind)
	})
	t.Run("el motor solo lo nombra en el mensaje", func(t *testing.T) {
		fake := newFakeDockerReplying(t, `{"jsonrpc":"2.0","id":"1","error":{"code":-32602,"message":"json: unknown field \"Contexto\"","data":{"tipo":"parametros_invalidos"}}}`)

		_, err := fake.engine.Attempt(context.Background(), testSpec, req, nil)

		var engineErr *application.EngineError
		require.ErrorAs(t, err, &engineErr)
		assert.Equal(t, application.EngineContextUnsupported, engineErr.Kind)
	})
	t.Run("otro parámetro inválido sigue siendo eso", func(t *testing.T) {
		fake := newFakeDockerReplying(t, `{"jsonrpc":"2.0","id":"1","error":{"code":-32602,"message":"ambiente inválido","data":{"tipo":"parametros_invalidos","campo":"Ambiente","valor":"x y"}}}`)

		_, err := fake.engine.Attempt(context.Background(), testSpec, req, nil)

		var engineErr *application.EngineError
		require.ErrorAs(t, err, &engineErr)
		assert.Equal(t, application.EngineInvalidParams, engineErr.Kind)
	})
	t.Run("sin haberlo enviado no se reinterpreta nada", func(t *testing.T) {
		fake := newFakeDockerReplying(t, unknownField)

		_, err := fake.engine.Attempt(context.Background(), testSpec, testAttempt, nil)

		var engineErr *application.EngineError
		require.ErrorAs(t, err, &engineErr)
		assert.Equal(t, application.EngineInvalidParams, engineErr.Kind)
	})
}

func TestVisibles(t *testing.T) {
	t.Run("el resultado trae las variables que el pipeline pidió mostrar", func(t *testing.T) {
		fake := newFakeDockerReplying(t, `{"jsonrpc":"2.0","id":"1","result":{"Intento":"i1","Estado":"exitoso","Despliegue":"d1",`+
			`"Detalle":{"Tiempo":"1s","Pasos":[]},`+
			`"Visibles":[{"Nombre":"app_url","Descripcion":"URL de la app","Valor":"https://app.example"},{"Nombre":"region","Descripcion":"","Valor":"sur"}]}}`)

		result, err := fake.engine.Attempt(context.Background(), testSpec, testAttempt, nil)

		require.NoError(t, err)
		assert.Equal(t, []application.VisibleOutput{
			{Name: "app_url", Description: "URL de la app", Value: "https://app.example"},
			{Name: "region", Value: "sur"},
		}, result.Visible)
	})
	t.Run("lista vacía o motor anterior: sin visibles y sin error", func(t *testing.T) {
		for _, visibles := range []string{`,"Visibles":[]`, ``} {
			fake := newFakeDockerReplying(t, `{"jsonrpc":"2.0","id":"1","result":{"Intento":"i1","Estado":"exitoso","Detalle":{"Tiempo":"1s","Pasos":[]}`+visibles+`}}`)

			result, err := fake.engine.Attempt(context.Background(), testSpec, testAttempt, nil)

			require.NoError(t, err)
			assert.Nil(t, result.Visible)
		}
	})
	t.Run("los visibles del progreso no rompen el evento", func(t *testing.T) {
		msg := &protocol.Message{Method: protocol.MethodProgreso, Params: json.RawMessage(
			`{"evento":"paso_terminado","intento":"i1","paso":"deploy","estado":"ejecutado","visibles":[{"nombre":"app_url","valor":"https://app.example"}]}`)}

		event, ok := toEngineEvent(msg)

		require.True(t, ok)
		assert.Equal(t, application.EngineEvent{
			Kind: application.StepFinished, AttemptID: "i1", Step: "deploy", StepStatus: application.StepExecuted,
		}, event)
	})
}

// El token de nube llega al contenedor como un archivo en un directorio montado de solo lectura. Lo que se ve
// desde fuera del contenedor (el argv de `docker run`, visible en `ps` y en `docker inspect`) no lleva ni el
// token ni ninguna variable del contrato: esas viajan por stdin, en el entorno de la petición.
func TestDockerEngine_ElTokenDeNubeNoViajaPorArgvNiPorVariables(t *testing.T) {
	const jwt = "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJ2ZXg6cDoxIn0.ZmlybWEtZGUtcHJ1ZWJh"
	credentials := filepath.Join(t.TempDir(), "credenciales", "run1")
	require.NoError(t, os.MkdirAll(credentials, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(credentials, application.TokenFileName), []byte(jwt), 0o600))

	spec := testSpec
	spec.Mounts = append(append([]application.Mount(nil), testSpec.Mounts...),
		application.Mount{Source: credentials, Target: application.ContainerCredentialsDir, ReadOnly: true})
	req := testAttempt
	req.Context = "azure/v1:3f9a1c0d2b7e4a65"
	req.Secrets = map[string]string{
		"TOKEN":                      "s3cr3t",
		"AZURE_CLIENT_ID":            "33333333-3333-4333-8333-333333333333",
		"AZURE_FEDERATED_TOKEN_FILE": application.ContainerTokenPath,
		"VEX_CLOUD_CONTRACT":         "azure/v1",
	}
	fake := newFakeDocker(t, "success")
	// En Linux el contenedor corre como el usuario del anfitrión: el único -e que eso añade es HOME.
	fake.engine.platform = func() platform {
		return platform{goos: "linux", uid: func() int { return 1000 }, gid: func() int { return 1000 }, rootless: func() bool { return false }}
	}

	_, err := fake.engine.Attempt(context.Background(), spec, req, nil)
	require.NoError(t, err)

	rawArgs := fake.readFile(t, "args.json")
	var args []string
	require.NoError(t, json.Unmarshal([]byte(rawArgs), &args))

	assert.Contains(t, args, "type=bind,source="+credentials+",target=/vex/credenciales,readonly",
		"el directorio del token se monta de solo lectura")
	assert.NotContains(t, rawArgs, jwt, "el token no va en argv")
	assert.NotContains(t, rawArgs, "eyJ", "ni nada con forma de JWT")
	for name, value := range req.Secrets {
		for _, arg := range args {
			assert.False(t, strings.HasPrefix(arg, name+"="), "%s no puede ir por -e", name)
		}
		if name != "AZURE_FEDERATED_TOKEN_FILE" && name != "VEX_CLOUD_CONTRACT" { // rutas e identificadores del contrato, no secretos
			assert.NotContains(t, rawArgs, value, "el valor de %s no va en argv", name)
		}
	}
	var envFlags []string
	for i, arg := range args {
		if arg == "-e" {
			envFlags = append(envFlags, args[i+1])
		}
	}
	assert.Equal(t, []string{"HOME=/home/vex", "VEX_ALMACEN=/vex/almacen"}, envFlags,
		"por -e solo van HOME y las rutas del motor")
	assert.Equal(t, "vex-runtime:latest", args[len(args)-1], "la imagen es el último argumento")

	rawRequest := fake.readFile(t, "request.json")
	assert.NotContains(t, rawRequest, jwt, "el token tampoco va en la petición: solo existe como archivo")
	var sent struct{ Entorno map[string]string }
	require.NoError(t, json.Unmarshal([]byte(rawRequest), &sent))
	assert.Equal(t, req.Secrets, sent.Entorno, "las variables del contrato viajan por stdin")
}
