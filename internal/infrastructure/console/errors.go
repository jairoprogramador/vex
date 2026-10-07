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
	var ambiguous *application.AmbiguousIDError
	var unknownEnvironment *application.UnknownEnvironmentError
	var wrongEnvironment *application.WrongEnvironmentError
	var unknownStep *application.UnknownStepError
	var checkFailed *application.CheckFailedError
	var openAttempts *application.OpenAttemptsError
	var finished *application.AttemptFinishedError
	switch {
	case errors.Is(err, application.ErrDeploymentRequired):
		return "Indica el despliegue al que volver.\n→ vex deployments <ambiente> los lista"
	case errors.Is(err, ErrNotInteractive):
		return "Este comando pide confirmación y no hay una terminal interactiva.\n→ Añade -y para confirmar sin preguntar"
	case errors.Is(err, application.ErrNoFailedAttempt):
		return "No recuerdo ningún intento fallido en este proyecto.\n→ vex ls <ambiente> muestra los intentos; pasa uno: vex why <id>"
	case errors.Is(err, application.ErrNoOpenAttempt):
		return "No recuerdo ningún intento sin terminar en este proyecto.\n→ vex ls <ambiente> muestra los intentos; si uno quedó en curso: vex abandon <id>"
	case errors.As(err, &openAttempts):
		return describeOpenAttempts(openAttempts)
	case errors.As(err, &finished):
		return describeFinishedAttempt(finished)
	case errors.Is(err, application.ErrNoRecentAttempt):
		return "Todavía no has ejecutado nada en este proyecto.\n→ Ejecuta un paso con: vex <paso> <ambiente>, o pasa el id: vex show <id>"
	case errors.Is(err, application.ErrIDTooShort):
		return "El id es demasiado corto: escribe al menos 6 caracteres del final.\n→ vex ls <ambiente> muestra los ids"
	case errors.Is(err, application.ErrUnknownID):
		return "No encuentro ese id entre los que conozco.\n→ vex ls <ambiente> lista los intentos y vex deployments <ambiente> los despliegues (release y rollback piden un despliegue). También vale el id completo"
	case errors.As(err, &ambiguous):
		return describeAmbiguousID(ambiguous)
	case errors.As(err, &unknownEnvironment):
		return describeUnknownEnvironment(unknownEnvironment)
	case errors.As(err, &wrongEnvironment):
		return describeWrongEnvironment(wrongEnvironment)
	case errors.As(err, &unknownStep):
		return describeUnknownStep(unknownStep)
	case errors.As(err, &checkFailed):
		return describeCheckFailure(checkFailed)
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

func describeAmbiguousID(e *application.AmbiguousIDError) string {
	short := make([]string, len(e.Candidates))
	for i, id := range e.Candidates {
		short[i] = ShortID(id)
	}
	return fmt.Sprintf("El id «%s» coincide con %d (%s).\n→ Escribe más caracteres del final para distinguirlos",
		e.Ref, len(e.Candidates), strings.Join(short, ", "))
}

func describeUnknownEnvironment(e *application.UnknownEnvironmentError) string {
	message := fmt.Sprintf("El ambiente «%s» no existe en este pipeline.", e.Name)
	if e.Suggestion != "" {
		message += fmt.Sprintf("\n¿Quisiste decir «%s»?", e.Suggestion)
	}
	if len(e.Known) > 0 {
		message += "\nDisponibles: " + strings.Join(e.Known, ", ")
		message += "\n→ vex envs lista los ambientes del pipeline (y actualiza esta lista si acabas de añadir uno)"
	}
	return message
}

func describeOpenAttempts(e *application.OpenAttemptsError) string {
	lines := make([]string, len(e.Candidates))
	for i, a := range e.Candidates {
		lines[i] = fmt.Sprintf("  %s  %s", ShortID(a.ID), a.Environment)
	}
	return fmt.Sprintf("Hay %d intentos sin terminar y no sé cuál abandonar:\n%s\n→ Indica cuál: vex abandon <id>",
		len(e.Candidates), strings.Join(lines, "\n"))
}

func describeFinishedAttempt(e *application.AttemptFinishedError) string {
	short := ShortID(e.AttemptID)
	var message string
	switch {
	case e.Abandoned:
		message = fmt.Sprintf("El intento %s ya se abandonó.", short)
	case e.Status == application.AttemptSucceeded:
		message = fmt.Sprintf("El intento %s ya terminó bien: no ocupa ningún ambiente.", short)
	case e.Status == application.AttemptCanceled:
		message = fmt.Sprintf("El intento %s ya se canceló: no ocupa ningún ambiente.", short)
	default:
		message = fmt.Sprintf("El intento %s ya terminó (fallido): no ocupa ningún ambiente.", short)
	}
	return message + "\n→ Solo se abandonan los intentos que quedaron sin terminar"
}

func describeWrongEnvironment(e *application.WrongEnvironmentError) string {
	return fmt.Sprintf("El despliegue %s es de %s, no de %s: solo se lanza en el ambiente donde se hizo.\n→ vex deployments %s lista los de ese ambiente",
		ShortID(e.DeploymentID), e.Actual, e.Requested, e.Requested)
}

func describeUnknownStep(e *application.UnknownStepError) string {
	message := fmt.Sprintf("El paso «%s» no existe en este pipeline.", e.Name)
	if e.Suggestion != "" {
		message += fmt.Sprintf("\n¿Quisiste decir «%s»?", e.Suggestion)
	}
	if len(e.Known) > 0 {
		message += "\nPasos: " + strings.Join(e.Known, " → ")
		message += "\n→ vex steps los lista"
	}
	return message
}

// describeCheckFailure explica por qué un paso no está listo para ejecutarse, sin haber ejecutado nada.
func describeCheckFailure(e *application.CheckFailedError) string {
	if len(e.Missing) > 0 {
		lines := make([]string, len(e.Missing))
		for i, m := range e.Missing {
			lines[i] = fmt.Sprintf("Faltan variables en el paso «%s»: %s", m.Step, strings.Join(m.Variables, ", "))
		}
		return strings.Join(lines, "\n") +
			fmt.Sprintf("\n→ Decláralas en el pipeline (compartidas, o en variables/%s/) o haz que las produzca un paso anterior", e.Environment)
	}
	return describeRejection(&application.EngineError{Failures: e.Failures}) +
		"\n→ Corrígelo en el repositorio del pipeline y vuelve a comprobar con: vex check " + e.UntilStep + " " + e.Environment
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
		return "El motor no encontró lo que se le pidió." + detail(e.Message) + "\n→ vex ls <ambiente> y vex deployments <ambiente> muestran lo que existe"
	case application.EngineUnavailable:
		return "El espacio de trabajo no está disponible; el intento no empezó." + detail(e.Message)
	case application.EngineUnknownOperation:
		return "La imagen runtime que usas es anterior a esta función.\n" +
			"  Actualiza `runtime.image` en vexconfig.yaml a una imagen con un vexd más reciente."
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
