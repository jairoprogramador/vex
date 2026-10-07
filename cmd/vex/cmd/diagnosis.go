package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/jairoprogramador/vex/internal/application"
	"github.com/jairoprogramador/vex/internal/infrastructure/console"
	"github.com/jairoprogramador/vex/internal/infrastructure/factories"
)

// Comandos que explican y arreglan un fallo: why y abandon.

type diagnoser interface {
	Why(ctx context.Context, ref, againstDeployment string) (application.WhyReport, error)
}

// newDiagnoser construye el servicio real; los tests lo reemplazan.
var newDiagnoser = func() (diagnoser, error) {
	return factories.NewServiceFactory().BuildDiagnosisService()
}

type abandoner interface {
	Plan(ctx context.Context, ref string) (application.AbandonPlan, error)
	Abandon(ctx context.Context, plan application.AbandonPlan) error
}

var newAbandoner = func() (abandoner, error) {
	return factories.NewServiceFactory().BuildAbandonService()
}

// confirmer pregunta antes de una acción que no se puede deshacer.
type confirmer interface {
	Confirm(question string) (bool, error)
}

// newConfirmer pregunta por stderr y lee de stdin; los tests lo reemplazan.
var newConfirmer = func() confirmer { return console.NewConfirmer(os.Stdin, os.Stderr) }

var (
	vsFlag  string
	yesFlag bool
)

var whyCmd = &cobra.Command{
	Use:   "why [intento]",
	Short: "Explica por qué falló un intento: qué cambió desde la última vez que funcionó (si la hubo)",
	Long: `Compara un intento que falló con la última vez que algo funcionó en ese ambiente y dice qué cambió: el código,
las instrucciones de un paso o sus variables.

Sin argumentos explica el último intento fallido de este proyecto. El id puede ser el completo o los últimos
caracteres (mínimo 6), tal como lo ves en "vex ls". Con --vs compara con un despliegue concreto.

Si no cambió nada de lo que mira cada paso, el fallo no viene de un cambio en el pipeline: mira lo externo
(credenciales, red, servicios) con "vex log --failed".`,
	Example: "  vex why\n  vex why efac5ab\n  vex why efac5ab --vs 91f4b55",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runLocalCommand(cmd, newDiagnoser, func(ctx context.Context, d diagnoser, views *console.Views) error {
			report, err := d.Why(ctx, optionalArg(args), vsFlag)
			if err != nil {
				return err
			}
			views.Why(report)
			return nil
		})
	},
}

var abandonCmd = &cobra.Command{
	Use:   "abandon [intento]",
	Short: "Libera un ambiente que un intento muerto dejó ocupado",
	Long: `Da por perdido un intento que quedó sin terminar (por ejemplo, porque el contenedor murió) y libera su ambiente.

Normalmente no hace falta: el motor recupera solo el ambiente de un proceso caído al lanzar otro intento. Úsalo si
no quieres esperar. Ojo: abandonar NO detiene los comandos que el proceso de ese intento siga ejecutando.

Sin argumentos usa el único intento sin terminar que se recuerda en este proyecto; si hay varios, hay que
indicar cuál: "vex ls <ambiente>" muestra los que siguen en curso. Pide confirmación (-y para no preguntar).`,
	Example: "  vex abandon\n  vex abandon 4f91b55 -y",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runLocalCommand(cmd, newAbandoner, func(ctx context.Context, a abandoner, views *console.Views) error {
			plan, err := a.Plan(ctx, optionalArg(args))
			if err != nil {
				return err
			}
			views.AbandonWarning(plan)
			if !yesFlag {
				confirmed, err := newConfirmer().Confirm("¿Abandonar este intento?")
				if err != nil {
					return err
				}
				if !confirmed {
					fmt.Fprintln(cmd.ErrOrStderr(), "No se abandonó nada.")
					return nil
				}
			}
			if err := a.Abandon(ctx, plan); err != nil {
				return err
			}
			views.Abandoned(plan)
			return nil
		})
	},
}

func init() {
	whyCmd.Flags().StringVar(&vsFlag, "vs", "", "Compara con este despliegue en vez del último anterior del ambiente")
	abandonCmd.Flags().BoolVarP(&yesFlag, "yes", "y", false, "No pedir confirmación")

	vexCmd.AddCommand(whyCmd, abandonCmd)
}
