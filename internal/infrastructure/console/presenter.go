// Package console muestra a la persona usuaria el avance de una ejecución.
package console

import (
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

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
	// plain evita los emojis: salidas sin color (NO_COLOR, tuberías, CI) no los llevan.
	plain bool
	// cloud es la conexión de nube del intento; nil si las credenciales las pone el usuario.
	cloud *application.CloudNotice
}

var _ application.ExecutionPresenter = (*Presenter)(nil)

func NewPresenter(out, errOut io.Writer) *Presenter {
	return &Presenter{out: out, errOut: errOut, plain: color.NoColor}
}

func (p *Presenter) Info(message string) {
	fmt.Fprintf(p.out, "• %s\n", message)
}

func (p *Presenter) Warn(message string) {
	fmt.Fprintf(p.errOut, "%s %s\n", warnMark, message)
}

func (p *Presenter) CloudConnected(connection application.CloudNotice) {
	p.cloud = &connection
	p.Info(fmt.Sprintf("Conexión de nube activa (%s · %s) · detalle: vex bootstrap %s --check",
		connection.Contract, shortConnectionID(connection.ConnectionID), connection.Environment))
}

// shortConnectionID es el comienzo del id de una conexión: basta para reconocerla sin mostrarla entera.
func shortConnectionID(id string) string {
	return id[:min(len(id), 8)]
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
	if result.Status != application.AttemptSucceeded {
		return
	}
	for _, visible := range result.Visible {
		p.visible(visible)
	}
	if p.cloud != nil {
		fmt.Fprintf(p.out, "  Listo en %s · sin secretos guardados\n", elapsed(result.Elapsed))
	}
}

// visible muestra una variable que el pipeline pidió mostrar. Una URL se destaca: suele ser lo que la persona
// quiere abrir al terminar. El pipeline decide el valor, así que se limpia antes de escribirlo en la terminal.
func (p *Presenter) visible(v application.VisibleOutput) {
	value := printable(v.Value)
	if !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
		fmt.Fprintf(p.out, "  %s: %s\n", printable(v.Name), value)
		return
	}
	rocket := "🚀"
	if p.plain {
		rocket = "->"
	}
	fmt.Fprintf(p.out, "  %s %s\n", rocket, value)
}

// printable quita los caracteres de control: un valor no debe poder mover el cursor ni cambiar colores.
func printable(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r) {
			return -1
		}
		return r
	}, text)
}

// elapsed muestra el tiempo total con la forma compacta de un cronómetro: 42s, 8m02s, 1h05m09s.
func elapsed(d time.Duration) string {
	total := int(d.Round(time.Second).Seconds())
	hours, minutes, seconds := total/3600, total%3600/60, total%60
	switch {
	case hours > 0:
		return fmt.Sprintf("%dh%02dm%02ds", hours, minutes, seconds)
	case minutes > 0:
		return fmt.Sprintf("%dm%02ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

func (p *Presenter) Failure(attemptID string, outputs []application.CommandOutput) {
	hints := p.cloudHints()
	for _, output := range outputs {
		fmt.Fprintf(p.errOut, "\n%s %s › %s\n%s\n",
			failMark, output.Step, output.Command, strings.TrimRight(output.Text, "\n"))
		for _, hint := range hints.next(output.Text) {
			fmt.Fprintf(p.errOut, "→ %s\n", hint)
		}
	}
	if attemptID != "" {
		short := ShortID(attemptID)
		fmt.Fprintf(p.errOut, "→ vex why %s · vex log %s --failed\n", short, short)
	}
}

// cloudHints son las instrucciones de la nube del intento; ninguna si las credenciales las pone el usuario.
func (p *Presenter) cloudHints() *hintPrinter {
	if p.cloud == nil {
		return &hintPrinter{}
	}
	return &hintPrinter{hints: p.cloud.Hints, environment: p.cloud.Environment}
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
