package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jairoprogramador/vex/internal/application"
	"github.com/jairoprogramador/vex/internal/infrastructure/console"
	"github.com/jairoprogramador/vex/internal/infrastructure/factories"
)

// rollbacker es lo que el comando necesita de la aplicación.
type rollbacker interface {
	Plan(ref string) (application.RollbackPlan, error)
	Run(ctx context.Context, plan application.RollbackPlan) error
}

// newRollbacker construye el servicio real; los tests lo reemplazan.
var newRollbacker = func() (rollbacker, error) {
	return factories.NewServiceFactory().BuildRollbackService()
}

var rollbackCmd = &cobra.Command{
	Use:   "rollback <despliegue>",
	Short: "Vuelve a desplegar los mismos commits de un despliegue anterior (crea un despliegue nuevo)",
	Long: `Vuelve a desplegar el proyecto y el pipeline tal como estaban en un despliegue anterior: el motor ejecuta TODOS
los pasos del pipeline con los mismos commits de entonces. El despliegue nuevo queda como hijo del anterior; el
historial no se reescribe. Es distinto de "vex release": release solo cambia cuál despliegue está visible, sin
ejecutar nada.

El id del despliegue puede ser el completo o los últimos caracteres (mínimo 6), tal como lo ves en
"vex deployments <ambiente>". Pide confirmación (-y para no preguntar).

Para que funcione, el repositorio del proyecto y el del pipeline tienen que seguir teniendo esos commits.`,
	Example: "  vex deployments prod\n  vex rollback 91f4b55\n  vex rollback 91f4b55 -y",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("indica el despliegue: vex rollback <despliegue>")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		return runLocalCommand(cmd, newRollbacker, func(ctx context.Context, r rollbacker, views *console.Views) error {
			plan, err := r.Plan(args[0])
			if err != nil {
				return err
			}
			views.RollbackWarning(plan)
			if !yesFlag {
				confirmed, err := newConfirmer().Confirm("¿Continuar?")
				if err != nil {
					return err
				}
				if !confirmed {
					fmt.Fprintln(cmd.ErrOrStderr(), "No se hizo nada.")
					return nil
				}
			}
			return r.Run(ctx, plan)
		})
	},
}

func init() {
	rollbackCmd.Flags().BoolVarP(&yesFlag, "yes", "y", false, "No pedir confirmación")

	vexCmd.AddCommand(rollbackCmd)
}
