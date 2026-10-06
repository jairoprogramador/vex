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
