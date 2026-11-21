package azure

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/jairoprogramador/vex/internal/application"
)

// Runner ejecuta la CLI de Azure con un argv explícito y devuelve su salida estándar. Nunca pasa por un shell:
// cada argumento llega tal cual a `az`.
type Runner interface {
	Run(ctx context.Context, args ...string) ([]byte, error)
}

var (
	// ErrNotInstalled: no hay `az` en el PATH.
	ErrNotInstalled = errors.New("no encuentro la CLI de Azure (az). Instálala: https://aka.ms/installazurecli")
	// ErrNoSession: `az` no tiene una sesión iniciada.
	ErrNoSession = errors.New("no hay una sesión de Azure en este equipo. Ejecuta `az login` y vuelve a intentarlo")
	// errOutputTooLarge: `az` devolvió más de lo que vex está dispuesto a leer.
	errOutputTooLarge = errors.New("la CLI de Azure devolvió una salida demasiado grande")
)

// CommandError es un `az` que terminó con error. Stderr ya viene acotado y sin caracteres de control: la salida
// de `az` es dato no confiable y acaba en la terminal.
type CommandError struct {
	// Command es el comando sin sus opciones ("deployment sub create").
	Command string
	Stderr  string
}

func (e *CommandError) Error() string {
	if e.Stderr == "" {
		return fmt.Sprintf("`az %s` falló", e.Command)
	}
	return fmt.Sprintf("`az %s` falló: %s", e.Command, e.Stderr)
}

const (
	maxStdoutBytes = 8 << 20
	maxStderrBytes = 16 << 10
	maxErrorText   = 800
)

// execRunner es el `az` real.
type execRunner struct{}

func (execRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "az", args...)
	// Solo errores por stderr y sin colores: lo que se lee es JSON y lo que se muestra es texto plano.
	cmd.Env = append(os.Environ(), "AZURE_CORE_ONLY_SHOW_ERRORS=true", "AZURE_CORE_NO_COLOR=true")
	stdout := &cappedBuffer{limit: maxStdoutBytes}
	stderr := &cappedBuffer{limit: maxStderrBytes}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	// Al cancelar, `az` recibe una interrupción y unos segundos para terminar antes de que se le mate.
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 10 * time.Second

	err := cmd.Run()
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return nil, ErrNotInstalled
	case ctx.Err() != nil:
		return nil, ctx.Err()
	case stdout.overflow:
		return nil, errOutputTooLarge
	case err != nil:
		return nil, &CommandError{Command: commandLabel(args), Stderr: clean(redact(stderr.String()), maxErrorText)}
	}
	return stdout.Bytes(), nil
}

// cappedBuffer guarda hasta limit bytes y descarta el resto, anotando que hubo más.
type cappedBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - b.Len(); len(p) > room {
		b.overflow = true
		b.Buffer.Write(p[:max(room, 0)])
		return len(p), nil
	}
	return b.Buffer.Write(p)
}

// commandLabel es el comando sin opciones ni valores, para nombrarlo en un error.
func commandLabel(args []string) string {
	var words []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			break
		}
		words = append(words, arg)
	}
	return strings.Join(words, " ")
}

// tokenLike es la forma de un JWT o de un token de portador; nada así debe llegar a la terminal.
var tokenLike = regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}(\.[A-Za-z0-9_-]+)*|(?i)(bearer|sig=|accountkey=|password=|client_secret=)\s*[^\s&;"']+`)

func redact(text string) string { return tokenLike.ReplaceAllString(text, "[oculto]") }

// clean deja un texto que viene de la plantilla o de la nube listo para mostrarse: una línea, acotada y solo con
// ASCII imprimible; lo demás, escapado a la vista. Así una plantilla hostil no puede falsificar la vista previa.
func clean(text string, limit int) string { return application.ExternalText(text, limit) }

// shellQuote deja un valor listo para pegarse en un shell dentro de un comando impreso.
func shellQuote(value string) string {
	if value != "" && !strings.ContainsFunc(value, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._/:-", r))
	}) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// commandLine es un comando de `az` tal como lo escribiría la persona.
func commandLine(args ...string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = shellQuote(arg)
	}
	return "az " + strings.Join(quoted, " ")
}
