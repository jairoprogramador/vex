package cmd

import (
	"github.com/jairoprogramador/vex/internal/infrastructure/factories"
	"github.com/spf13/cobra"
)

var nonInteractive bool

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initializes a new project (creates vexconfig.yaml).",
	RunE: func(cmd *cobra.Command, args []string) error {
		factory := factories.NewServiceFactory()

		initService, err := factory.BuildInitialize()
		if err != nil {
			return err
		}
		return initService.Run(cmd.Context(), !nonInteractive)
	},
}

func init() {
	initCmd.Flags().BoolVarP(&nonInteractive, "yes", "y", false, "Use default values without prompting for input.")
}
