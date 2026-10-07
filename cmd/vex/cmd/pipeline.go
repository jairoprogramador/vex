package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jairoprogramador/vex/internal/application"
	"github.com/jairoprogramador/vex/internal/infrastructure/console"
	"github.com/jairoprogramador/vex/internal/infrastructure/factories"
)

// Comandos que miran el pipeline sin ejecutarlo: envs, steps y check.

// pipelineInspector es lo que estos comandos necesitan de la aplicación.
type pipelineInspector interface {
	Environments(ctx context.Context) ([]application.Environment, error)
	Steps(ctx context.Context) ([]application.PipelineStep, error)
	Check(ctx context.Context, step, environment string) error
}

// newPipelineInspector construye el servicio real; los tests lo reemplazan.
var newPipelineInspector = func() (pipelineInspector, error) {
	return factories.NewServiceFactory().BuildPipelineService()
}

func runPipeline(cmd *cobra.Command, run func(ctx context.Context, p pipelineInspector, views *console.Views) error) error {
	return runLocalCommand(cmd, newPipelineInspector, run)
}

// reservedCommandNames son los nombres que `vex` ya usa como comando (con sus alias): un paso del pipeline que se
// llame igual queda tapado y hay que ejecutarlo con `vex run`.
func reservedCommandNames() map[string]bool {
	names := map[string]bool{}
	for _, c := range vexCmd.Commands() {
		names[c.Name()] = true
		for _, alias := range c.Aliases {
			names[alias] = true
		}
	}
	return names
}

var envsCmd = &cobra.Command{
	Use:   "envs",
	Short: "Lista los ambientes del pipeline y cuáles están protegidos",
	Long: `Lista los ambientes del pipeline, en su orden, y cómo se lanza cada uno: automático (el motor lanza cada
despliegue) o manual/protegido (lo decides tú con "vex release"). El valor de la primera columna es el que usas en
los comandos, por ejemplo "vex test sand".`,
	Example: "  vex envs",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runPipeline(cmd, func(ctx context.Context, p pipelineInspector, views *console.Views) error {
			environments, err := p.Environments(ctx)
			if err != nil {
				return err
			}
			views.Environments(environments)
			return nil
		})
	},
}

var stepsCmd = &cobra.Command{
	Use:   "steps",
	Short: "Lista los pasos del pipeline, en orden",
	Long: `Lista los pasos del pipeline, en el orden en que se ejecutan. "vex <paso> <ambiente>" ejecuta hasta ese paso,
y también los anteriores que hagan falta.`,
	Example: "  vex steps",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runPipeline(cmd, func(ctx context.Context, p pipelineInspector, views *console.Views) error {
			steps, err := p.Steps(ctx)
			if err != nil {
				return err
			}
			views.Steps(steps, reservedCommandNames())
			return nil
		})
	},
}

var checkCmd = &cobra.Command{
	Use:   "check <paso> <ambiente>",
	Short: "Comprueba que todo está bien, sin ejecutar nada",
	Long: `Comprueba que el pipeline es válido y que ese paso se puede ejecutar en ese ambiente: que el ambiente y el paso
existen y que las variables que usan los pasos se resuelven. No ejecuta ningún comando ni deja nada en el historial.

Ojo: que pase no garantiza que la ejecución salga bien (un comando puede fallar al correr), solo que no fallará
por un error de configuración.`,
	Example: "  vex check deploy sand\n  vex check test prod",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 2 {
			return fmt.Errorf("indica el paso y el ambiente: vex check <paso> <ambiente>")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		step, environment := args[0], args[1]
		return runPipeline(cmd, func(ctx context.Context, p pipelineInspector, views *console.Views) error {
			if err := p.Check(ctx, step, environment); err != nil {
				return err
			}
			views.CheckPassed(environment, step)
			if reservedCommandNames()[step] {
				fmt.Fprintf(cmd.ErrOrStderr(), "! El paso «%s» se llama igual que un comando de vex: ejecútalo con `vex run %s <ambiente>`.\n", step, step)
			}
			return nil
		})
	},
}

func init() {
	vexCmd.AddCommand(envsCmd, stepsCmd, checkCmd)
}
