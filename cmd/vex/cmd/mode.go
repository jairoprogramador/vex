package cmd

import (
	"fmt"
	"os"

	survey "github.com/AlecAivazis/survey/v2"
	"github.com/jairoprogramador/vex/internal/config"
	"github.com/spf13/cobra"
)

func surveyIcons() survey.AskOpt {
	return survey.WithIcons(func(icons *survey.IconSet) {
		icons.Question.Text = "→"
		icons.Question.Format = "cyan+b"
		icons.SelectFocus.Text = "▸"
		icons.SelectFocus.Format = "green+b"
	})
}

var modeCmd = &cobra.Command{
	Use:   "mode",
	Short: "Selecciona interactivamente el modo de ejecución",
	Long: `Presenta un selector para elegir el modo de ejecución (remote/local)
y el scope donde guardar el valor (project/user/global).

Equivalente interactivo de: vex config mode=<valor> --scope <scope>`,
	Args: cobra.NoArgs,
	RunE: runModeSelect,
}

func runModeSelect(cmd *cobra.Command, _ []string) error {
	var selectedMode string
	if err := survey.AskOne(&survey.Select{
		Message: "Modo de ejecución:",
		Options: []string{"remote", "local"},
		Default: "remote",
		Description: func(value string, _ int) string {
			if value == "remote" {
				return "ejecuta en el portal vex"
			}
			return "ejecuta en Docker local"
		},
	}, &selectedMode, surveyIcons()); err != nil {
		return err
	}

	var selectedScope string
	if err := survey.AskOne(&survey.Select{
		Message: "Guardar en scope:",
		Options: []string{"project", "user", "global"},
		Default: "project",
		Description: func(value string, _ int) string {
			switch value {
			case "project":
				return "vexconfig.yaml (solo este proyecto)"
			case "user":
				return "~/.vex/config (todos los proyectos del usuario)"
			default:
				return "ruta de sistema (todos los usuarios)"
			}
		},
	}, &selectedScope, surveyIcons()); err != nil {
		return err
	}

	projectPath, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve project path: %w", err)
	}

	scope := config.Scope(selectedScope)
	cfg := config.Config{Mode: config.ExecutionMode(selectedMode)}
	if err := config.SaveScope(scope, cfg, projectPath); err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("%w\n(sugerencia: para scope global usa sudo)", err)
		}
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "mode = %s  (scope: %s)\n", selectedMode, scope)
	return nil
}
