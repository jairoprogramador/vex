package console

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// ErrNotInteractive: el comando pide confirmación y no hay una persona al otro lado (un script, un CI).
var ErrNotInteractive = errors.New("hace falta confirmar y no hay una terminal interactiva")

// Confirmer pregunta «¿continuar? [s/N]». La respuesta por defecto es no: solo un sí explícito confirma.
type Confirmer struct {
	in          io.Reader
	out         io.Writer
	interactive bool
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
