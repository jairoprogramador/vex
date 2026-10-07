package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jairoprogramador/vex/internal/application"
	"github.com/jairoprogramador/vex/internal/config"
	"github.com/jairoprogramador/vex/internal/infrastructure/factories"
	"github.com/spf13/cobra"
)

var (
	modeFlag string
	noFollow bool
	noCheck  bool
	version  string
)

var vexCmd = &cobra.Command{
	Use:   "vex",
	Short: "Ejecuta el pipeline de tu proyecto en un ambiente y consulta qué pasó",
	Long: `vex ejecuta el pipeline de tu proyecto (test, package, deploy…) en un ambiente (sand, stag, prod…), guarda
el historial de cada intento y te ayuda a entender qué pasó.

Empieza así:
  vex init                  crea vexconfig.yaml en tu proyecto
  vex envs                  ve los ambientes y vex steps los pasos del pipeline
  vex check deploy sand     comprueba que todo está listo, sin ejecutar nada
  vex deploy sand           ejecuta hasta el paso "deploy" en "sand"  (atajo de: vex run deploy sand)
  vex why                   si falló: qué cambió desde la última vez que funcionó

Palabras que verás en todos los comandos:
  paso         una etapa del pipeline (test, package, deploy…); vex steps los lista
  ambiente     dónde se ejecuta (sand, stag, prod…); vex envs los lista
  intento      cada vez que ejecutas un paso; queda en el historial, salga bien o mal
  despliegue   un intento que llegó hasta el final con éxito; es lo que se puede lanzar o recuperar
  lanzamiento  marcar un despliegue como el visible en su ambiente (solo, o a mano con vex release)

Los comandos de "Consultar" y "Desplegar y lanzar" aceptan el id completo de un intento o despliegue, o sus
últimos caracteres (mínimo 6). Sin id, usan el último intento de este proyecto.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			if cmd.HasSubCommands() && cmd.CalledAs() == "vex" {
				return nil
			}
			return errors.New("indica el paso y el ambiente: vex <paso> <ambiente> (vex --help lista los comandos)")
		}
		if len(args) > 2 {
			return errors.New("sobran argumentos: vex <paso> <ambiente>")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		return runStep(cmd, args)
	},
}

// runCmd es la forma explícita de ejecutar un paso: `vex run <paso> <ambiente>`. Hace lo mismo que
// `vex <paso> <ambiente>`, pero sin ambigüedad cuando un paso del pipeline se llama igual que un comando
// (un paso `release` o `log` quedaría tapado por el comando del mismo nombre).
var runCmd = &cobra.Command{
	Use:   "run <paso> <ambiente>",
	Short: "Ejecuta el pipeline hasta ese paso en ese ambiente",
	Long: `Ejecuta el pipeline hasta ese paso en ese ambiente. Se hacen también los pasos anteriores que hagan falta.

Es lo mismo que "vex <paso> <ambiente>"; usa "vex run" cuando el nombre de un paso coincida con un comando
de vex (por ejemplo, un paso llamado "release").`,
	Example: "  vex run test sand\n  vex run release prod",
	Args:    cobra.RangeArgs(1, 2),
	RunE:    func(cmd *cobra.Command, args []string) error { return runStep(cmd, args) },
}

// newRunner construye quien ejecuta el paso; los tests lo reemplazan.
var newRunner = func(mode config.ExecutionMode, follow, preflight bool) (application.Runner, error) {
	return factories.NewServiceFactory().BuildRunner(mode, follow, preflight)
}

// runStep ejecuta un paso del pipeline en un ambiente: args es [paso] o [paso, ambiente].
func runStep(cmd *cobra.Command, args []string) error {
	step := args[0]
	environment := ""
	if len(args) > 1 {
		environment = args[1]
	}

	mode, err := resolveMode(modeFlag)
	if err != nil {
		return err
	}

	runner, err := newRunner(mode, !noFollow, !noCheck)
	if err != nil {
		return err
	}

	ctx := cmd.Context()
	if mode == config.ModeLocal {
		// Ctrl+C cancela el contexto: el ejecutor local le pide al motor que
		// cancele en vez de morir con el contenedor a medias.
		var stop context.CancelFunc
		ctx, stop = signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()
	}

	err = runner.Run(ctx, step, environment)
	if err != nil && mode == config.ModeLocal {
		reportLocalError(cmd, err)
	}
	return err
}

const noCheckUsage = "No comprobar antes de ejecutar que el paso está listo (ambiente, paso y variables). Ahorra unos 250 ms, pero un error de tecleo deja un intento fallido en el historial."

func resolveMode(flagVal string) (config.ExecutionMode, error) {
	if flagVal != "" {
		m := config.ExecutionMode(flagVal)
		if !m.IsValid() {
			return config.ModeUnset, fmt.Errorf(
				"--mode %q inválido: debe ser %q o %q", flagVal, config.ModeRemote, config.ModeLocal)
		}
		return m, nil
	}
	projectPath, err := os.Getwd()
	if err != nil {
		return config.ModeUnset, fmt.Errorf("resolve project path: %w", err)
	}
	effective, err := config.LoadEffective(projectPath)
	if err != nil {
		return config.ModeUnset, fmt.Errorf("load config: %w", err)
	}
	return effective.Mode, nil
}

func Execute(versionMain string) {
	version = versionMain
	vexCmd.Version = fmt.Sprintf("v%s\n", version)
	applyGroups(vexCmd)
	if err := vexCmd.Execute(); err != nil {
		os.Exit(exitCode(err))
	}
}

func init() {
	vexCmd.Flags().StringVar(&modeFlag, "mode", "",
		`Modo de ejecución: "local" (default) o "remote".
Si no se especifica, se lee de vexconfig.yaml (proyecto),
~/.vex/config (usuario) o la ruta de sistema (global),
en ese orden de prioridad. Ver 'vex config --help'.`)
	vexCmd.Flags().BoolVar(&noFollow, "no-follow", false, "Solo modo remoto: no seguir el log en vivo; termina en cuanto la ejecución queda en cola.")
	vexCmd.Flags().BoolVar(&noCheck, "no-check", false, noCheckUsage)
	runCmd.Flags().BoolVar(&noCheck, "no-check", false, noCheckUsage)
	runCmd.Flags().StringVar(&modeFlag, "mode", "", vexCmd.Flags().Lookup("mode").Usage)
	runCmd.Flags().BoolVar(&noFollow, "no-follow", false, vexCmd.Flags().Lookup("no-follow").Usage)
	vexCmd.SetVersionTemplate(`{{.Version}}`)

	vexCmd.AddCommand(runCmd)
	vexCmd.AddCommand(initCmd)
	vexCmd.AddCommand(archCmd)
	vexCmd.AddCommand(versionCmd)
	vexCmd.AddCommand(cancelCmd)
	vexCmd.AddCommand(configCmd)
	vexCmd.AddCommand(modeCmd)
	vexCmd.SilenceUsage = true
}
