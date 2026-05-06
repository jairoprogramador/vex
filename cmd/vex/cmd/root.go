package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jairoprogramador/vex/internal/infrastructure/factories"
	"github.com/spf13/cobra"
)

var (
	withTtyFlag bool
	colorFlag   string
	remoteFlag  bool
	noFollow    bool
	version     string
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
		if len(args) > 2 {
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

		runner, err := factory.BuildRunner(resolveRemote(remoteFlag), !noFollow)
		if err != nil {
			return err
		}

		return runner.Run(cmd.Context(), command, environment)
	},
}

// resolveRemote folds the `--remote` flag together with the VEX_MODE env
// var. The flag wins when set; otherwise VEX_MODE=remote opts the user in
// without retyping the flag on every invocation.
func resolveRemote(flag bool) bool {
	if flag {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(os.Getenv("VEX_MODE")), "remote")
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
	rootCmd.PersistentFlags().BoolVar(&withTtyFlag, "with-tty", false, "Enable pseudo-TTY allocation.")
	rootCmd.PersistentFlags().StringVar(&colorFlag, "color", "always", "control color output (auto, always, never)")
	// Local flags on the deploy invocation. Persistent would also work but
	// these only apply to `vex <step> [env]`, not to `vex auth/init/...`.
	rootCmd.Flags().BoolVar(&remoteFlag, "remote", false, "Run the deploy on the Vex portal infrastructure (Fly Machines) instead of the local Docker daemon. Equivalent to VEX_MODE=remote.")
	rootCmd.Flags().BoolVar(&noFollow, "no-follow", false, "When used with --remote, skip the live log stream and exit as soon as the execution is queued.")
	rootCmd.SetVersionTemplate(`{{.Version}}`)

	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(architectureCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(authCmd)
	rootCmd.AddCommand(executionCmd)
	rootCmd.SilenceUsage = true
}
