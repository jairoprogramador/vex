package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/jairoprogramador/vex/internal/application"
	"github.com/jairoprogramador/vex/internal/infrastructure/console"
	"github.com/jairoprogramador/vex/internal/infrastructure/factories"
)

// bootstrapService es lo que `vex bootstrap` necesita de la aplicación.
type bootstrapService interface {
	Plan(ctx context.Context, opts application.BootstrapOptions) (*application.BootstrapPlan, error)
	Apply(ctx context.Context, plan *application.BootstrapPlan, acceptance application.Acceptance) (application.BootstrapResult, error)
	Check(ctx context.Context, environment string) (application.CheckReport, error)
}

// acceptor pregunta a la persona. Aceptar permisos en la nube no tiene modo desatendido.
type acceptor interface {
	Interactive() bool
	Confirm(question string) (bool, error)
	ConfirmTyped(question, expected string) (string, error)
}

// Los tests reemplazan el servicio y a quien responde.
var (
	newBootstrapService = func() (bootstrapService, error) { return factories.NewServiceFactory().BuildBootstrapService() }
	newAcceptor         = func() acceptor { return console.NewConfirmer(os.Stdin, os.Stderr) }
)

var errBootstrapNeedsTerminal = errors.New(
	"vex bootstrap aplica permisos en tu nube y alguien tiene que revisarlos: ejecútalo en una terminal interactiva " +
		"(no hay modo desatendido)\n→ Para auditar una conexión sin cambiar nada: vex bootstrap <ambiente> --check")

var (
	bootstrapLocation  string
	bootstrapCheck     bool
	bootstrapReconnect bool
)

var bootstrapCmd = &cobra.Command{
	Use:   "bootstrap <ambiente>",
	Short: "Conecta tu nube con vex para un ambiente: aplica el bootstrap del pipeline y crea la confianza",
	Long: `Conecta la nube de un ambiente con vex, con tu propia sesión de la nube (para Azure: az login). Tu sesión no sale
de tu equipo: vex solo recibe identificadores.

Qué hace, en orden:
  1. Lee el bootstrap del pipeline (bootstrap/<nube>/) en el commit configurado y comprueba que no cree confianzas,
     no ejecute scripts y no traiga plantillas de fuera.
  2. Te muestra con qué cuenta, sobre qué suscripción, para qué proyecto y qué cambiaría, con los permisos como
     «identidad → rol → ámbito». No aplica nada sin tu confirmación; los permisos resaltados (Owner, User Access
     Administrator, Role Based Access Control Administrator, ámbito de suscripción, para una identidad o sobre algo
     que el bootstrap no crea, o indeterminados) y lo que la nube no supo previsualizar se aceptan tecleando el
     nombre del ambiente.
  3. Aplica la plantilla y comprueba que la identidad de despliegue es la que creó el bootstrap y que no tiene más
     permisos que los que viste; si los tiene, te los muestra y solo sigue si los aceptas por escrito.
  4. Crea la credencial federada «vex-oidc» sobre esa identidad y espera a que el portal verifique la conexión
     canjeando un token real.

Volver a ejecutarlo retoma lo que quedó a medias. Si el bootstrap del pipeline cambió, reconecta: la conexión anterior
sigue activa en el portal hasta que la nueva se verifica.

Con --check no cambia nada: audita la conexión punto por punto y sale con error si algo no está como vex lo dejó.`,
	Example: "  az login\n  vex bootstrap sand --location eastus2\n  vex bootstrap sand --check",
	Args:    environmentArg("vex bootstrap <ambiente>"),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCloudCommand(cmd, func(ctx context.Context, views *console.Views) error {
			service, err := newBootstrapService()
			if err != nil {
				return err
			}
			if bootstrapCheck {
				return runBootstrapCheck(ctx, service, views, args[0])
			}
			return runBootstrap(ctx, cmd, service, views, application.BootstrapOptions{
				Environment: args[0], Region: bootstrapLocation, Reconnect: bootstrapReconnect,
			})
		})
	},
}

func runBootstrap(
	ctx context.Context, cmd *cobra.Command, service bootstrapService, views *console.Views, opts application.BootstrapOptions,
) error {
	answers := newAcceptor()
	// Antes de abrir nada en el portal o en la nube: sin nadie que revise, no se empieza.
	if !answers.Interactive() {
		return errBootstrapNeedsTerminal
	}
	plan, err := service.Plan(ctx, opts)
	if err != nil {
		return err
	}
	views.BootstrapPlan(plan)

	acceptance := application.Acceptance{}
	if plan.NeedsAcceptance() {
		accepted, err := askAcceptance(answers, plan, &acceptance)
		if err != nil {
			return err
		}
		if !accepted {
			fmt.Fprintln(cmd.ErrOrStderr(), "No se aplicó nada. La conexión abierta en el portal vence sola en 30 minutos.")
			return nil
		}
	}
	// Con la plantilla aplicada y antes de crear la confianza, vex puede encontrar que la identidad de despliegue
	// trae permisos que nadie vio: se enseñan y solo se aceptan por escrito.
	acceptance.ConfirmExposure = func(grants []application.PlannedGrant) (string, error) {
		views.TrustExposure(plan.Environment, grants)
		return answers.ConfirmTyped("Estos permisos no estaban en lo que aceptaste.", plan.Environment)
	}
	result, err := service.Apply(ctx, plan, acceptance)
	if err != nil {
		return err
	}
	if plan.Mode != application.BootstrapUpToDate {
		views.BootstrapDone(result)
	}
	return nil
}

// askAcceptance pide la confirmación que el plan exige: escrita si hay permisos resaltados, sí/no en otro caso.
func askAcceptance(answers acceptor, plan *application.BootstrapPlan, acceptance *application.Acceptance) (bool, error) {
	if expected := plan.TypedConfirmation(); expected != "" {
		typed, err := answers.ConfirmTyped("Este bootstrap incluye permisos resaltados o recursos sin vista previa.", expected)
		if err != nil {
			return false, err
		}
		acceptance.Typed = typed
		return typed == expected, nil
	}
	confirmed, err := answers.Confirm("¿Aplicar este bootstrap en tu nube?")
	acceptance.Confirmed = confirmed
	return confirmed, err
}

func runBootstrapCheck(ctx context.Context, service bootstrapService, views *console.Views, environment string) error {
	report, err := service.Check(ctx, environment)
	if err != nil {
		return err
	}
	views.CheckReport(report)
	if report.Failed() {
		return application.ErrCheckFoundProblems
	}
	return nil
}

// runCloudCommand ejecuta un comando que administra la conexión de nube: Ctrl+C lo cancela y los errores salen
// con el formato de la CLI. No depende del modo de ejecución: no habla con el motor.
func runCloudCommand(cmd *cobra.Command, run func(ctx context.Context, views *console.Views) error) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, console.NewViews(cmd.OutOrStdout())); err != nil {
		reportLocalError(cmd, err)
		return err
	}
	return nil
}

func init() {
	bootstrapCmd.Flags().StringVar(&bootstrapLocation, "location", "",
		"Región de la nube donde aplicar (por ejemplo, eastus2). Obligatoria la primera vez; después se usa la de la conexión.")
	bootstrapCmd.Flags().BoolVar(&bootstrapCheck, "check", false,
		"Solo audita la conexión, sin cambiar nada: confianza, huella del bootstrap, vista previa sin cambios y canje.")
	bootstrapCmd.Flags().BoolVar(&bootstrapReconnect, "reconnect", false,
		"Rehace la conexión aunque el bootstrap no haya cambiado (la anterior sigue activa hasta que la nueva se verifica).")
	bootstrapCmd.MarkFlagsMutuallyExclusive("check", "reconnect")
	bootstrapCmd.MarkFlagsMutuallyExclusive("check", "location")

	vexCmd.AddCommand(bootstrapCmd)
}
