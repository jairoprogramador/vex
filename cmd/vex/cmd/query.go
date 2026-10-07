package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/jairoprogramador/vex/internal/application"
	"github.com/jairoprogramador/vex/internal/config"
	"github.com/jairoprogramador/vex/internal/infrastructure/console"
	"github.com/jairoprogramador/vex/internal/infrastructure/factories"
)

// Comandos que consultan el historial del motor: ls, show, log, deployments y releases. No ejecutan nada.

// queries es lo que estos comandos necesitan de la aplicación.
type queries interface {
	ListAttempts(ctx context.Context, environment string, limit int) (application.AttemptList, error)
	ShowAttempt(ctx context.Context, ref string) (application.AttemptDetail, error)
	Logs(ctx context.Context, ref string, onlyFailed bool) (application.AttemptLogs, error)
	ListDeployments(ctx context.Context, environment string, limit int) (application.DeploymentList, error)
	ListReleases(ctx context.Context, environment string, limit int) (application.ReleaseList, error)
}

// newQueries construye el servicio real; los tests lo reemplazan.
var newQueries = func() (queries, error) {
	return factories.NewServiceFactory().BuildQueryService()
}

var errQueryNeedsLocalMode = errors.New(
	"este comando consulta el motor local y estás en modo remoto\n→ Para usar el modo local: vex config mode=local")

const defaultLimit = 10

var (
	limitFlag  int
	allFlag    bool
	failedFlag bool
)

// queryMode es el modo con el que se resuelve un comando que habla con el motor; los tests lo reemplazan.
var queryMode = func() (config.ExecutionMode, error) { return resolveMode("") }

// runLocalCommand ejecuta un comando que habla con el motor local y pinta su respuesta: exige el modo local,
// construye el servicio y deja que Ctrl+C lo cancele. Un error se muestra con su mensaje amigable.
func runLocalCommand[S any](
	cmd *cobra.Command, build func() (S, error), run func(ctx context.Context, service S, views *console.Views) error,
) error {
	mode, err := queryMode()
	if err != nil {
		return err
	}
	if mode != config.ModeLocal {
		return errQueryNeedsLocalMode
	}
	service, err := build()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, service, console.NewViews(cmd.OutOrStdout())); err != nil {
		reportLocalError(cmd, err)
		return err
	}
	return nil
}

// runQuery ejecuta una consulta al historial.
func runQuery(cmd *cobra.Command, query func(ctx context.Context, q queries, views *console.Views) error) error {
	return runLocalCommand(cmd, newQueries, query)
}

// limit es cuántos elementos mostrar: --all los muestra todos (0), -n elige cuántos.
func limit() int {
	if allFlag {
		return 0
	}
	return limitFlag
}

func environmentArg(usage string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("indica el ambiente: %s", usage)
		}
		return nil
	}
}

func addListFlags(c *cobra.Command) {
	c.Flags().IntVarP(&limitFlag, "limit", "n", defaultLimit, "Cuántos mostrar, los más recientes primero")
	c.Flags().BoolVar(&allFlag, "all", false, "Mostrar todos")
}

var lsCmd = &cobra.Command{
	Use:     "ls <ambiente>",
	Aliases: []string{"list"},
	Short:   "Muestra los últimos intentos de un ambiente",
	Long: `Muestra los últimos intentos de un ambiente: cuál fue, hasta qué paso llegó, cómo terminó, quién lo pidió y cuándo.
Los más recientes salen primero.`,
	Example: "  vex ls sand\n  vex ls prod -n 20\n  vex ls prod --all",
	Args:    environmentArg("vex ls <ambiente>"),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runQuery(cmd, func(ctx context.Context, q queries, views *console.Views) error {
			list, err := q.ListAttempts(ctx, args[0], limit())
			if err != nil {
				return err
			}
			views.AttemptList(list)
			return nil
		})
	},
}

var showCmd = &cobra.Command{
	Use:   "show [intento]",
	Short: "Muestra qué pasó en un intento, paso a paso",
	Long: `Muestra qué pasó en un intento: cómo terminó, cuánto tardó y qué pasó en cada paso.
Sin argumentos muestra el último intento que ejecutaste en este proyecto. El id puede ser el completo o los últimos
caracteres (mínimo 6), tal como lo ves en "vex ls".`,
	Example: "  vex show\n  vex show 5ab306c",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runQuery(cmd, func(ctx context.Context, q queries, views *console.Views) error {
			detail, err := q.ShowAttempt(ctx, optionalArg(args))
			if err != nil {
				return err
			}
			views.AttemptDetail(detail)
			return nil
		})
	},
}

var logCmd = &cobra.Command{
	Use:     "log [intento]",
	Aliases: []string{"logs"},
	Short:   "Muestra la salida de los comandos de un intento",
	Long: `Muestra lo que imprimió cada comando de un intento. Con --failed, solo los que fallaron.
Sin argumentos usa el último intento que ejecutaste en este proyecto.`,
	Example: "  vex log\n  vex log --failed\n  vex log 5ab306c",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runQuery(cmd, func(ctx context.Context, q queries, views *console.Views) error {
			logs, err := q.Logs(ctx, optionalArg(args), failedFlag)
			if err != nil {
				return err
			}
			views.Logs(logs, failedFlag)
			return nil
		})
	},
}

var deploymentsCmd = &cobra.Command{
	Use:     "deployments <ambiente>",
	Aliases: []string{"deploys"},
	Short:   "Lista los despliegues de un ambiente y marca el que está lanzado",
	Long: `Lista los despliegues de un ambiente. Un despliegue nace cuando un intento ejecuta TODOS los pasos del pipeline
con éxito. La columna LANZAMIENTO dice cuál está visible ahora (●).`,
	Example: "  vex deployments prod\n  vex deploys prod --all",
	Args:    environmentArg("vex deployments <ambiente>"),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runQuery(cmd, func(ctx context.Context, q queries, views *console.Views) error {
			list, err := q.ListDeployments(ctx, args[0], limit())
			if err != nil {
				return err
			}
			views.DeploymentList(list)
			return nil
		})
	},
}

var releasesCmd = &cobra.Command{
	Use:   "releases <ambiente>",
	Short: "Muestra el historial de lanzamientos de un ambiente",
	Long: `Muestra cuándo se lanzó cada despliegue en un ambiente (el lanzamiento es lo que hace visible un despliegue).
El primero es el que está lanzado ahora.`,
	Example: "  vex releases prod",
	Args:    environmentArg("vex releases <ambiente>"),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runQuery(cmd, func(ctx context.Context, q queries, views *console.Views) error {
			list, err := q.ListReleases(ctx, args[0], limit())
			if err != nil {
				return err
			}
			views.ReleaseList(list)
			return nil
		})
	},
}

func optionalArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

func init() {
	addListFlags(lsCmd)
	addListFlags(deploymentsCmd)
	addListFlags(releasesCmd)
	logCmd.Flags().BoolVar(&failedFlag, "failed", false, "Mostrar solo los comandos que fallaron")

	vexCmd.AddCommand(lsCmd, showCmd, logCmd, deploymentsCmd, releasesCmd)
}
