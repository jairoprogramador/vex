package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/jairoprogramador/vex/internal/infrastructure/factory"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "vex [paso] [ambiente]",
	Short: "Vex es una herramienta CLI para automatizar despliegues.",
	Long:  `Una herramienta para orquestar despliegues de software a través de diferentes ambientes`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) > 2 {
			return errors.New("se requiere un paso y opcionalmente un ambiente")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		finalStepName := args[0]
		environment := ""
		if len(args) == 2 {
			environment = args[1]
		}

		factoryApp, err := factory.NewFactory()
		if err != nil {
			return err
		}

		orchestrator, err := factoryApp.BuildExecutionOrchestrator()
		if err != nil {
			return err
		}
		err = orchestrator.ExecutePlan(context.Background(), finalStepName, environment)
		if err != nil {
			return err
		}
		return nil
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.Version = fmt.Sprintf("vex version: %s\n", version)
	rootCmd.SetVersionTemplate(`{{.Version}}`)

	rootCmd.PersistentFlags().String("color", "always", "control color output (auto, always, never)")
	viper.BindPFlag("color", rootCmd.PersistentFlags().Lookup("color"))

	//rootCmd.AddCommand(logCmd)

	cobra.OnInitialize(initConfig)
}

func initConfig() {
	switch viper.GetString("color") {
	case "always":
		color.NoColor = false
	case "never":
		color.NoColor = true
	default:
		color.NoColor = false
	}
}
