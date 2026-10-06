package console

import (
	"bytes"
	"testing"

	"github.com/fatih/color"
	"github.com/stretchr/testify/assert"

	"github.com/jairoprogramador/vex/internal/application"
)

func newTestPresenter(t *testing.T) (*Presenter, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	// Sin colores la salida es estable y comparable.
	previous := color.NoColor
	color.NoColor = true
	okMark, failMark, warnMark = "✔", "✘", "!"
	t.Cleanup(func() { color.NoColor = previous })

	var out, errOut bytes.Buffer
	return NewPresenter(&out, &errOut), &out, &errOut
}

func TestPresenter_AvanceDeUnIntento(t *testing.T) {
	p, out, _ := newTestPresenter(t)

	p.Info("Preparando proyecto: u@main")
	p.Event(application.EngineEvent{Kind: application.AttemptStarted, AttemptID: "i1"})
	p.Event(application.EngineEvent{Kind: application.StepStarted, Step: "test"})
	p.Event(application.EngineEvent{Kind: application.CommandFinished, Step: "test", Command: "mvn test", Succeeded: true})
	p.Event(application.EngineEvent{Kind: application.StepFinished, Step: "test", StepStatus: application.StepReused})
	p.Result(application.AttemptResult{AttemptID: "i1", Status: application.AttemptSucceeded, Duration: "1s"})

	assert.Equal(t, "• Preparando proyecto: u@main\n"+
		"Intento i1\n"+
		"▶ test\n"+
		"  ✔ mvn test\n"+
		"  ✔ test: reutilizado\n"+
		"\n✔ Intento i1: exitoso (1s)\n", out.String())
}

func TestPresenter_MuestraElDespliegueSiLoHay(t *testing.T) {
	p, out, _ := newTestPresenter(t)

	p.Result(application.AttemptResult{AttemptID: "i1", Status: application.AttemptSucceeded, Duration: "1s", DeploymentID: "d7"})

	assert.Equal(t, "\n✔ Intento i1: exitoso (1s)\n  Despliegue: d7\n", out.String())
}

func TestPresenter_Fallos(t *testing.T) {
	p, out, errOut := newTestPresenter(t)

	p.Event(application.EngineEvent{Kind: application.CommandFinished, Command: "mvn test", Succeeded: false})
	p.Result(application.AttemptResult{AttemptID: "i1", Status: application.AttemptFailed, Duration: "2s"})
	p.Warn("falta ARM_ID")
	p.FailedCommands([]application.CommandOutput{{Step: "test", Command: "mvn test", Text: "boom\n\n"}})

	assert.Equal(t, "  ✘ mvn test\n\n✘ Intento i1: fallido (2s)\n", out.String())
	assert.Equal(t, "! falta ARM_ID\n\n✘ test › mvn test\nboom\n", errOut.String(),
		"avisos y salida fallida van a stderr, sin saltos de línea de más")
}

func TestPresenter_EstadosDesconocidosSeMuestranTalCual(t *testing.T) {
	assert.Equal(t, "otro", stepLabel("otro"))
	assert.Equal(t, "raro", attemptLabel("raro"))
}
