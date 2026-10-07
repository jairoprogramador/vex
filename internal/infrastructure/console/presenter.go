// Package console muestra a la persona usuaria el avance de una ejecución.
package console

import (
	"fmt"
	"io"
	"strings"

	"github.com/fatih/color"

	"github.com/jairoprogramador/vex/internal/application"
)

var (
	okMark   = color.New(color.FgGreen).Sprint("✔")
	failMark = color.New(color.FgRed).Sprint("✘")
	warnMark = color.New(color.FgYellow).Sprint("!")
)

// Presenter escribe en out el avance de la ejecución. Los avisos van a errOut.
type Presenter struct {
	out    io.Writer
	errOut io.Writer
}

var _ application.ExecutionPresenter = (*Presenter)(nil)

func NewPresenter(out, errOut io.Writer) *Presenter {
	return &Presenter{out: out, errOut: errOut}
}

func (p *Presenter) Info(message string) {
	fmt.Fprintf(p.out, "• %s\n", message)
}

func (p *Presenter) Warn(message string) {
	fmt.Fprintf(p.errOut, "%s %s\n", warnMark, message)
}

func (p *Presenter) Event(event application.EngineEvent) {
	switch event.Kind {
	case application.AttemptStarted:
		fmt.Fprintf(p.out, "Intento %s\n", event.AttemptID)
	case application.StepStarted:
		fmt.Fprintf(p.out, "▶ %s\n", event.Step)
	case application.CommandFinished:
		fmt.Fprintf(p.out, "  %s %s\n", mark(event.Succeeded), event.Command)
	case application.StepFinished:
		fmt.Fprintf(p.out, "  %s %s: %s\n", mark(isStepOK(event.StepStatus)), event.Step, stepLabel(event.StepStatus))
	}
}

func (p *Presenter) Result(result application.AttemptResult) {
	fmt.Fprintf(p.out, "\n%s Intento %s: %s (%s)\n",
		mark(result.Status == application.AttemptSucceeded), result.AttemptID, attemptLabel(result.Status), result.Duration)
	if result.DeploymentID != "" {
		fmt.Fprintf(p.out, "  Despliegue: %s\n", result.DeploymentID)
	}
}

func (p *Presenter) Failure(attemptID string, outputs []application.CommandOutput) {
	for _, output := range outputs {
		fmt.Fprintf(p.errOut, "\n%s %s › %s\n%s\n",
			failMark, output.Step, output.Command, strings.TrimRight(output.Text, "\n"))
	}
	if attemptID != "" {
		short := ShortID(attemptID)
		fmt.Fprintf(p.errOut, "→ vex why %s · vex log %s --failed\n", short, short)
	}
}

func mark(ok bool) string {
	if ok {
		return okMark
	}
	return failMark
}

func isStepOK(status application.StepStatus) bool {
	return status == application.StepExecuted || status == application.StepReused
}

func stepLabel(status application.StepStatus) string {
	switch status {
	case application.StepExecuted:
		return "ejecutado"
	case application.StepReused:
		return "reutilizado"
	case application.StepFailed:
		return "fallido"
	case application.StepCanceled:
		return "cancelado"
	}
	return string(status)
}

func attemptLabel(status application.AttemptStatus) string {
	switch status {
	case application.AttemptSucceeded:
		return "exitoso"
	case application.AttemptFailed:
		return "fallido"
	case application.AttemptCanceled:
		return "cancelado"
	}
	return string(status)
}
