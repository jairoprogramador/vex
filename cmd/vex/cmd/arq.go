package cmd

import (
	"github.com/jairoprogramador/vex/internal/infrastructure/factories"
	"github.com/spf13/cobra"
)

var archCmd = &cobra.Command{
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
