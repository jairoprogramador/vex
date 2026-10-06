package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jairoprogramador/vex/internal/application"
	"github.com/jairoprogramador/vex/internal/infrastructure/console"
)

// exitCodeCanceled es la convención de shell para una interrupción por Ctrl+C
// (128 + SIGINT); es también el código con el que sale el motor al cancelar.
const exitCodeCanceled = 130

// exitCode traduce el error final del comando en el código de salida del proceso.
func exitCode(err error) int {
	if errors.Is(err, application.ErrAttemptCanceled) {
		return exitCodeCanceled
	}
	return 1
}

// reportLocalError muestra el error de una ejecución local con el formato de
// la CLI y evita que cobra lo imprima por segunda vez con su "Error: ...".
func reportLocalError(cmd *cobra.Command, err error) {
	cmd.SilenceErrors = true
	if message := console.DescribeError(err); message != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: %s\n", message)
	}
}
