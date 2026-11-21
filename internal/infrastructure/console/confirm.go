package console

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/jairoprogramador/vex/internal/application"
)

// ErrNotInteractive: el comando pide confirmación y no hay una persona al otro lado (un script, un CI).
var ErrNotInteractive = errors.New("hace falta confirmar y no hay una terminal interactiva")

// Confirmer pregunta «¿continuar? [s/N]». La respuesta por defecto es no: solo un sí explícito confirma.
type Confirmer struct {
	in          io.Reader
	out         io.Writer
	interactive bool
}

// Interactive dice si hay una persona al otro lado a quien preguntar.
func (c *Confirmer) Interactive() bool { return c.interactive }

// ConfirmTyped pide teclear expected para aceptar algo que no debe confirmarse por inercia. Devuelve lo tecleado,
// sin espacios alrededor; quien llama decide si vale.
func (c *Confirmer) ConfirmTyped(question, expected string) (string, error) {
	if !c.interactive {
		return "", ErrNotInteractive
	}
	fmt.Fprintf(c.out, "%s\nEscribe %q para aceptar: ", question, expected)
	line, err := bufio.NewReader(c.in).ReadString('\n')
	if err != nil && line == "" && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// NewConfirmer lee de stdin y pregunta por out (stderr, para no mezclar la pregunta con la salida del comando).
// Sin terminal interactiva, Confirm devuelve ErrNotInteractive en vez de esperar una respuesta que no llegará.
func NewConfirmer(in *os.File, out io.Writer) *Confirmer {
	// ModeCharDevice no basta: /dev/null también lo es.
	return &Confirmer{in: in, out: out, interactive: term.IsTerminal(int(in.Fd()))}
}

// NewConfirmerWith permite elegir el origen y si es interactivo; sirve para probar.
func NewConfirmerWith(in io.Reader, out io.Writer, interactive bool) *Confirmer {
	return &Confirmer{in: in, out: out, interactive: interactive}
}

func (c *Confirmer) Confirm(question string) (bool, error) {
	if !c.interactive {
		return false, ErrNotInteractive
	}
	fmt.Fprintf(c.out, "%s [s/N] ", question)
	line, err := bufio.NewReader(c.in).ReadString('\n')
	if err != nil && line == "" {
		if errors.Is(err, io.EOF) {
			return false, nil // sin respuesta: no
		}
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "s", "si", "sí", "y", "yes":
		return true, nil
	}
	return false, nil
}

// ApprovePortalChanges enseña los cambios de pipeline o de imagen que propone el portal y pregunta si se adoptan.
// Sin terminal devuelve false sin preguntar: quien llama se detiene con un mensaje que incluye la diferencia.
func (c *Confirmer) ApprovePortalChanges(changes []application.ProjectChange) (bool, error) {
	if !c.interactive {
		return false, nil
	}
	fmt.Fprintf(c.out, "%s El portal propone cambiar en vexconfig.yaml de dónde sale lo que se ejecuta:\n", warnMark)
	for _, change := range changes {
		fmt.Fprintf(c.out, "  %s: %s → %s\n", change.Field, printable(change.Current), printable(change.Proposed))
	}
	fmt.Fprintln(c.out, "  Acéptalo solo si el cambio es tuyo (lo hiciste en el portal). Si no lo reconoces, responde no.")
	return c.Confirm("¿Escribir estos cambios en vexconfig.yaml y usarlos?")
}
