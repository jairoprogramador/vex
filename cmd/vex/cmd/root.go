package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/jairoprogramador/vex/internal/config"
	"github.com/jairoprogramador/vex/internal/infrastructure/factories"
	"github.com/spf13/cobra"
)

var (
	modeFlag string
	noFollow bool
	version  string
)

var rootCmd = &cobra.Command{
	Use:   "vex",
	Short: "vex is an opinionated CLI for designing production-ready cloud architectures",
	Long:  `Vex is a smart CLI that designs cloud architecture for you. Answer a few key questions about your workload, and Vex generates a production-ready infrastructure blueprint aligned with modern best practices.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			if cmd.HasSubCommands() && cmd.CalledAs() == "vex" {
				return nil
			}
			return errors.New("a step argument is required")
		}
		if len(args) > 3 {
			return errors.New("a maximum of two arguments are allowed: a step and an optional environment")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}

		command := args[0]
		environment := ""
		if len(args) > 1 {
			environment = args[1]
		}

		factory := factories.NewServiceFactory()

		mode, err := resolveMode(modeFlag)
		if err != nil {
			return err
		}

		runner, err := factory.BuildRunner(mode, !noFollow)
		if err != nil {
			return err
		}

		return runner.Run(cmd.Context(), command, environment)
	},
}

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
	rootCmd.Version = fmt.Sprintf("v%s\n", version)
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().StringVar(&modeFlag, "mode", "",
		`Modo de ejecución: "remote" (default) o "local".
Si no se especifica, se lee de vexconfig.yaml (proyecto),
~/.vex/config (usuario) o la ruta de sistema (global),
en ese orden de prioridad. Ver 'vex config --help'.`)
	rootCmd.Flags().BoolVar(&noFollow, "no-follow", false, "When used in remote mode, skip the live log stream and exit as soon as the execution is queued.")
	rootCmd.SetVersionTemplate(`{{.Version}}`)

	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(architectureCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(authCmd)
	rootCmd.AddCommand(cancelCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.SilenceUsage = true
}
