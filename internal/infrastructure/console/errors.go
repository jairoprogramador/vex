package console

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jairoprogramador/vex/internal/application"
)

// DescribeError traduce un error de la ejecución local al mensaje que ve la
// persona usuaria. Devuelve "" cuando no hay nada que añadir porque el detalle
// ya se mostró (un intento fallido muestra su propio resumen y salida).
func DescribeError(err error) string {
	var engineErr *application.EngineError
	switch {
	case errors.Is(err, application.ErrAttemptFailed):
		return ""
	case errors.Is(err, application.ErrAttemptCanceled):
		return "Ejecución cancelada."
	case errors.Is(err, application.ErrEnvironmentRequired):
		return "Falta el ambiente. Uso: vex <paso> <ambiente>"
	case errors.As(err, &engineErr):
		return describeEngineError(engineErr)
	}
	return err.Error()
}

func describeEngineError(e *application.EngineError) string {
	switch e.Kind {
	case application.EngineEnvironmentBusy:
		return fmt.Sprintf("El ambiente %q ya tiene un intento en curso (%s).\n"+
			"Espera a que termine. Si su proceso murió, el motor lo recupera solo: vuelve a intentarlo pasados unos 15 segundos.",
			e.Environment, e.AttemptID)
	case application.EnginePipelineRejected:
		return describeRejection(e)
	case application.EngineUnsupported:
		return "La imagen trae un vexd que no habla esta versión del protocolo.\n" +
			"Actualiza la imagen runtime (o la versión de vex)." + detail(e.Message)
	case application.EngineInvalidParams:
		return "El motor no aceptó la petición." + detail(e.Message)
	case application.EngineNotFound:
		return "El motor no encontró lo que se le pidió." + detail(e.Message)
	case application.EngineUnavailable:
		return "El espacio de trabajo no está disponible; el intento no empezó." + detail(e.Message)
	case application.EngineInvalidConfig:
		return "El motor está mal configurado dentro de la imagen " +
			"(faltan VEX_ALMACEN o VEX_ESPACIO, o el almacén no existe)." + detail(e.Message)
	case application.EngineConcurrentWrite:
		return "Otro proceso escribió el historial a la vez. Vuelve a intentarlo."
	case application.EngineNoAttempts:
		return "Todavía no hay intentos registrados."
	case application.EngineCanceled:
		return "Ejecución cancelada."
	case application.EngineInternal:
		return "Error interno del motor." + detail(e.Message) + detail(e.Stderr)
	case application.EngineDidNotRespond:
		return "No se pudo ejecutar el motor en el contenedor." + detail(e.Stderr) +
			"\nRevisa que Docker esté en ejecución y que la imagen exista."
	}
	return "Error del motor." + detail(e.Message)
}

func describeRejection(e *application.EngineError) string {
	switch {
	case len(e.Failures) > 0:
		lines := make([]string, len(e.Failures))
		for i, f := range e.Failures {
			lines[i] = fmt.Sprintf("  - %s: %s (%s)", location(f), f.Detail, f.Invariant)
		}
		return "El pipeline no pasa la comprobación:\n" + strings.Join(lines, "\n")
	case e.Variable != "":
		return fmt.Sprintf("La variable %q no está disponible para el comando (ambiente %q).\n"+
			"Revisa que el ambiente exista en el pipeline y que la variable esté declarada "+
			"(compartida o de ese ambiente) o la produzca un comando anterior.", e.Variable, e.Environment)
	}
	return "El motor rechazó la ejecución." + detail(e.Message)
}

func location(f application.PipelineFailure) string {
	if f.File != "" {
		return f.File
	}
	return f.Step
}

// detail añade un texto técnico indentado debajo del mensaje principal.
func detail(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return "\n  " + strings.ReplaceAll(text, "\n", "\n  ")
}
