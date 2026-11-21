package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jairoprogramador/vex/internal/application"
	"github.com/jairoprogramador/vex/internal/infrastructure/console"
	"github.com/jairoprogramador/vex/internal/infrastructure/factories"
)

// disconnector es lo que `vex disconnect` necesita de la aplicación.
type disconnector interface {
	Plan(ctx context.Context, environment string) (*application.DisconnectPlan, error)
	Run(ctx context.Context, plan *application.DisconnectPlan) (application.DisconnectResult, error)
}

// newDisconnector construye el servicio real; los tests lo reemplazan.
var newDisconnector = func() (disconnector, error) { return factories.NewServiceFactory().BuildDisconnectService() }

// errTrustStillInCloud: la conexión quedó revocada, pero la confianza de vex sigue en la nube. El detalle y los
// comandos ya se mostraron.
var errTrustStillInCloud = errors.New("la conexión quedó revocada, pero falta retirar la confianza de vex en la nube (comandos arriba)")

var disconnectYes bool

var disconnectCmd = &cobra.Command{
	Use:   "disconnect <ambiente>",
	Short: "Desconecta la nube de un ambiente: revoca la conexión y retira la confianza de vex",
	Long: `Corta la conexión de nube de un ambiente.

Primero revoca la conexión en el portal, que deja de emitir tokens; si el portal no lo confirma, se detiene sin tocar
la nube. Después, con tu sesión de la nube, borra la credencial federada «vex-oidc» de la identidad de despliegue.

No borra recursos ni permisos: son de quien aplicó el bootstrap. Al final imprime los comandos para borrarlos tú. Si un
bloqueo de la nube impide retirar la confianza, vex no lo quita: te dice qué comandos ejecutar.

El corte no es instantáneo: un token ya emitido vale hasta 10 minutos, y un token de la nube ya canjeado, más.`,
	Example: "  vex disconnect sand\n  vex disconnect sand -y",
	Args:    environmentArg("vex disconnect <ambiente>"),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCloudCommand(cmd, func(ctx context.Context, views *console.Views) error {
			service, err := newDisconnector()
			if err != nil {
				return err
			}
			plan, err := service.Plan(ctx, args[0])
			if err != nil {
				return err
			}
			views.DisconnectWarning(plan)
			if !disconnectYes {
				confirmed, err := newConfirmer().Confirm("¿Desconectar?")
				if err != nil {
					return err
				}
				if !confirmed {
					fmt.Fprintln(cmd.ErrOrStderr(), "No se hizo nada.")
					return nil
				}
			}
			result, err := service.Run(ctx, plan)
			if err != nil {
				return err
			}
			views.Disconnected(result)
			if !result.TrustRemoved {
				return errTrustStillInCloud
			}
			return nil
		})
	},
}

func init() {
	disconnectCmd.Flags().BoolVarP(&disconnectYes, "yes", "y", false, "No pedir confirmación")

	vexCmd.AddCommand(disconnectCmd)
}
