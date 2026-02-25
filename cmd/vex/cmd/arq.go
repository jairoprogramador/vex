package cmd

import (
	"github.com/jairoprogramador/vex-client/internal/infrastructure/factories"
	"github.com/spf13/cobra"
)

var architectureCmd = &cobra.Command{
	Use:   "arq",
	Short: "Designs a production-ready cloud architecture.",
	RunE: func(cmd *cobra.Command, args []string) error {
		factory := factories.NewServiceFactory()

		architectureService, err := factory.BuildArchitecture()
		if err != nil {
			return err
		}
		return architectureService.Run()
	},
}
