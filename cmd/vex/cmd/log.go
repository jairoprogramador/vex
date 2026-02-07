package cmd

import (
	"github.com/jairoprogramador/vex/internal/infrastructure/factory"
	"github.com/spf13/cobra"
)

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "Show the detailed log of the last execution",
	Long:  `Reads and displays the most recent log file from the .vex/logs directory.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		factoryApp, err := factory.NewFactory()
		if err != nil {
			return err
		}

		logService := factoryApp.BuildLogService()
		err = logService.ShowLog(factoryApp.PathAppProject())
		if err != nil {
			return err
		}

		return nil
	},
}
