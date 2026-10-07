package console

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jairoprogramador/vex/internal/application"
)

func TestDescribeError_ErroresPropiosDeLaAplicacion(t *testing.T) {
	assert.Empty(t, DescribeError(application.ErrAttemptFailed),
		"el fallo del intento ya se mostró con su resumen y su salida")
	assert.Empty(t, DescribeError(fmt.Errorf("envuelto: %w", application.ErrAttemptFailed)))
	assert.Equal(t, "Ejecución cancelada.", DescribeError(application.ErrAttemptCanceled))
	assert.Contains(t, DescribeError(application.ErrEnvironmentRequired), "vex <paso> <ambiente>")
}

func TestDescribeError_ErrorDesconocidoSeMuestraTalCual(t *testing.T) {
	assert.Equal(t, "preparar proyecto: sin acceso", DescribeError(errors.New("preparar proyecto: sin acceso")))
}

func TestDescribeError_ErroresDelMotor(t *testing.T) {
	tests := []struct {
		name string
		err  *application.EngineError
		want []string
	}{
		{
			name: "ambiente ocupado dice cuál y qué intento",
			err:  &application.EngineError{Kind: application.EngineEnvironmentBusy, Environment: "sand", AttemptID: "i9"},
			want: []string{`"sand"`, "i9", "recupera solo", "15 segundos"},
		},
		{
			name: "pipeline rechazado lista los fallos con su ubicación",
			err: &application.EngineError{Kind: application.EnginePipelineRejected, Failures: []application.PipelineFailure{
				{Invariant: "formato", File: "steps/01-test/commands.yaml", Detail: "no se puede leer"},
				{Invariant: "variables", Step: "acr", Detail: "usa una variable no declarada"},
			}},
			want: []string{
				"El pipeline no pasa la comprobación:",
				"  - steps/01-test/commands.yaml: no se puede leer (formato)",
				"  - acr: usa una variable no declarada (variables)",
			},
		},
		{
			name: "variable no disponible orienta a declararla",
			err:  &application.EngineError{Kind: application.EnginePipelineRejected, Variable: "etiqueta", Environment: "nope"},
			want: []string{`"etiqueta"`, `ambiente "nope"`, "declarada"},
		},
		{
			name: "rechazo sin datos muestra el texto del motor indentado",
			err: &application.EngineError{Kind: application.EnginePipelineRejected,
				Message: `el paso pedido "nope" no es un paso del pipeline`},
			want: []string{"El motor rechazó la ejecución.", `  el paso pedido "nope" no es un paso del pipeline`},
		},
		{
			name: "versión no soportada pide actualizar la imagen",
			err:  &application.EngineError{Kind: application.EngineUnsupported, Message: `versión no soportada: "9"`},
			want: []string{"Actualiza la imagen runtime", `"9"`},
		},
		{
			name: "fuente inválida",
			err:  &application.EngineError{Kind: application.EngineInvalidParams, Message: "la fuente /x no es un repositorio"},
			want: []string{"no aceptó la petición", "/x no es un repositorio"},
		},
		{
			name: "una imagen anterior a la operación pide actualizar runtime.image",
			err:  &application.EngineError{Kind: application.EngineUnknownOperation},
			want: []string{"anterior a esta función", "runtime.image"},
		},
		{
			name: "configuración inválida nombra las variables",
			err:  &application.EngineError{Kind: application.EngineInvalidConfig, Message: "falta VEX_ALMACEN"},
			want: []string{"VEX_ALMACEN", "VEX_ESPACIO"},
		},
		{
			name: "escritura concurrente sugiere reintentar",
			err:  &application.EngineError{Kind: application.EngineConcurrentWrite},
			want: []string{"Vuelve a intentarlo"},
		},
		{
			name: "error interno adjunta la causa del contenedor",
			err:  &application.EngineError{Kind: application.EngineInternal, Message: "interno", Stderr: "panic: boom"},
			want: []string{"Error interno del motor.", "  panic: boom"},
		},
		{
			name: "sin respuesta explica con la salida de docker",
			err: &application.EngineError{Kind: application.EngineDidNotRespond,
				Stderr: "Unable to find image 'x:latest' locally\nexit"},
			want: []string{"No se pudo ejecutar el motor", "  Unable to find image 'x:latest' locally", "  exit", "Docker esté en ejecución"},
		},
		{
			name: "tipo desconocido no se pierde",
			err:  &application.EngineError{Kind: application.EngineUnknown, Message: "algo nuevo"},
			want: []string{"Error del motor.", "algo nuevo"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DescribeError(fmt.Errorf("contexto: %w", tt.err))

			for _, fragment := range tt.want {
				assert.Contains(t, got, fragment)
			}
		})
	}
}

func TestDescribeError_ErroresDeLasConsultas(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want []string
	}{
		{"sin intentos recordados", application.ErrNoRecentAttempt, []string{"Todavía no has ejecutado nada", "vex <paso> <ambiente>"}},
		{"id demasiado corto", application.ErrIDTooShort, []string{"demasiado corto", "al menos 6", "vex ls"}},
		{"id desconocido", application.ErrUnknownID, []string{"No encuentro ese id", "vex ls", "id completo"}},
		{
			"id ambiguo lista las coincidencias cortas",
			&application.AmbiguousIDError{Ref: "123456", Candidates: []string{
				"01a113d2-0001-7000-8000-000000123456", "01a113d3-0002-7000-8000-ffffff123456"}},
			[]string{"«123456» coincide con 2", "0123456", "Escribe más caracteres"},
		},
		{
			"ambiente que no existe, con sugerencia y los disponibles",
			&application.UnknownEnvironmentError{Name: "sandd", Known: []string{"sand", "stag", "prod"}, Suggestion: "sand"},
			[]string{"«sandd» no existe", "¿Quisiste decir «sand»?", "Disponibles: sand, stag, prod", "→ vex envs"},
		},
		{
			"paso que no existe, con sugerencia y los pasos en orden",
			&application.UnknownStepError{Name: "pakage", Known: []string{"test", "package", "deploy"}, Suggestion: "package"},
			[]string{"«pakage» no existe", "¿Quisiste decir «package»?", "Pasos: test → package → deploy", "→ vex steps"},
		},
		{
			"variables que faltan: por paso y con dónde declararlas",
			&application.CheckFailedError{Environment: "sand", UntilStep: "deploy", Missing: []application.MissingVariables{
				{Step: "deploy", Variables: []string{"db_url", "api_key"}}, {Step: "package", Variables: []string{"tag"}}}},
			[]string{"Faltan variables en el paso «deploy»: db_url, api_key", "paso «package»: tag", "variables/sand/"},
		},
		{
			"un pipeline inválido lista los fallos y dice cómo volver a comprobar",
			&application.CheckFailedError{Environment: "sand", UntilStep: "deploy", Failures: []application.PipelineFailure{
				{Invariant: "formato", File: "steps/01-test/commands.yaml", Detail: "no se puede leer"}}},
			[]string{"El pipeline no pasa la comprobación:", "steps/01-test/commands.yaml: no se puede leer (formato)", "vex check deploy sand"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DescribeError(fmt.Errorf("contexto: %w", tt.err))

			for _, fragment := range tt.want {
				assert.Contains(t, got, fragment)
			}
		})
	}
}

func TestDescribeError_AmbienteSinSugerenciaNiLista(t *testing.T) {
	got := DescribeError(&application.UnknownEnvironmentError{Name: "x"})

	assert.Equal(t, "El ambiente «x» no existe en este pipeline.", got)
}

func TestDescribeError_ErroresDeWhyYAbandon(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want []string
	}{
		{"sin confirmación posible", ErrNotInteractive, []string{"pide confirmación", "-y"}},
		{"despliegue de otro ambiente", &application.WrongEnvironmentError{DeploymentID: idFailed, Actual: "sand", Requested: "prod"}, []string{"efac5ab es de sand, no de prod", "vex deployments prod"}},
		{"rollback sin despliegue", application.ErrDeploymentRequired, []string{"Indica el despliegue", "vex deployments <ambiente>"}},
		{"sin fallidos recordados", application.ErrNoFailedAttempt, []string{"ningún intento fallido", "vex why <id>"}},
		{"sin abiertos recordados", application.ErrNoOpenAttempt, []string{"ningún intento sin terminar", "vex abandon <id>"}},
		{
			"varios abiertos: se listan para que elija",
			&application.OpenAttemptsError{Candidates: []application.RecentAttempt{
				{ID: "01a113d0-7999-7000-8000-000004f91b55", Environment: "sand"},
				{ID: "01a113d0-7a00-7000-8000-0000000d3c1e", Environment: "prod"},
			}},
			[]string{"2 intentos sin terminar", "4f91b55  sand", "00d3c1e  prod", "vex abandon <id>"},
		},
		{"ya abandonado", &application.AttemptFinishedError{AttemptID: "01a113d0-7999-7000-8000-000004f91b55", Abandoned: true}, []string{"4f91b55 ya se abandonó"}},
		{"ya terminó bien", &application.AttemptFinishedError{AttemptID: "01a113d0-7999-7000-8000-000004f91b55", Status: application.AttemptSucceeded}, []string{"ya terminó bien", "no ocupa ningún ambiente"}},
		{"ya terminó fallido", &application.AttemptFinishedError{AttemptID: "01a113d0-7999-7000-8000-000004f91b55", Status: application.AttemptFailed}, []string{"ya terminó (fallido)"}},
		{"ya se canceló", &application.AttemptFinishedError{AttemptID: "01a113d0-7999-7000-8000-000004f91b55", Status: application.AttemptCanceled}, []string{"ya se canceló"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DescribeError(fmt.Errorf("contexto: %w", tt.err))

			for _, fragment := range tt.want {
				assert.Contains(t, got, fragment)
			}
		})
	}
}
