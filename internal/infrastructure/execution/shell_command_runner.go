package execution

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strings"

	"github.com/jairoprogramador/vex/internal/domain/execution/ports"
	"github.com/jairoprogramador/vex/internal/domain/execution/vos"
)

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// ShellCommandRunner es una implementación de CommandRunner que ejecuta comandos a través del shell del sistema.
type ShellCommandRunner struct{}

// NewShellCommandRunner crea una nueva instancia de ShellCommandRunner.
func NewShellCommandRunner() ports.CommandRunner {
	return &ShellCommandRunner{}
}

// Run ejecuta un comando en el shell apropiado para el sistema operativo.
func (r *ShellCommandRunner) Run(ctx context.Context, command string, workDir string) (*vos.CommandResult, error) {
	var cmd *exec.Cmd

	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}

	cmd.Dir = workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	// Primero, preparamos el resultado con las salidas, ya que siempre las queremos.
	result := &vos.CommandResult{
		RawStdout:        stdout.String(),
		RawStderr:        stderr.String(),
		NormalizedStdout: strings.TrimSpace(ansiRegex.ReplaceAllString(strings.ReplaceAll(stdout.String(), "\r\n", "\n"), "")),
		NormalizedStderr: strings.TrimSpace(ansiRegex.ReplaceAllString(strings.ReplaceAll(stderr.String(), "\r\n", "\n"), "")),
	}

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// Es un error de código de salida. No es un error de ejecución del runner.
			// Populamos el código de salida y devolvemos el resultado sin error.
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}
		// Es un error diferente (ej. comando no encontrado). Esto sí es un error del runner.
		return nil, fmt.Errorf("error al intentar ejecutar el comando '%s': %w", command, err)
	}

	// Si no hubo error, el código de salida es 0.
	result.ExitCode = 0
	return result, nil
}
