package cmd

import (
	"context"
	"errors"

	"github.com/spf13/cobra"

	"github.com/jairoprogramador/vex/internal/application"
	"github.com/jairoprogramador/vex/internal/infrastructure/console"
	"github.com/jairoprogramador/vex/internal/infrastructure/factories"
)

// Comandos que deciden qué despliegue está visible: release, protect y unprotect.

type releaser interface {
	Release(ctx context.Context, environment, ref, name string) (application.Release, error)
	Protect(ctx context.Context, environment string) error
	Unprotect(ctx context.Context, environment string) error
}

// newReleaser construye el servicio real; los tests lo reemplazan.
var newReleaser = func() (releaser, error) {
	return factories.NewServiceFactory().BuildReleaseService()
}

var releaseName string

// exactArgs exige n argumentos y, si no están, dice cómo se usa el comando en vez del mensaje genérico de cobra.
func exactArgs(n int, usage string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != n {
			return errors.New(usage)
		}
		return nil
	}
}

var releaseCmd = &cobra.Command{
	Use:   "release <ambiente> <despliegue>",
	Short: "Hace visible un despliegue en un ambiente",
	Long: `Lanza un despliegue sin ejecutar nada (para volver a ejecutar, vex rollback): lo marca como el que está visible en el ambiente. Es lo que hace solo cada despliegue
exitoso, salvo en los ambientes protegidos (vex protect), donde lo decides tú con este comando.

El despliegue tiene que ser del mismo ambiente. El id puede ser el completo o los últimos caracteres (mínimo 6),
tal como lo ves en "vex deployments <ambiente>". Con --name le das un nombre a la versión.`,
	Example: "  vex deployments prod\n  vex release prod 91f4b55\n  vex release prod 91f4b55 --name v2.1",
	Args:    exactArgs(2, "indica el ambiente y el despliegue: vex release <ambiente> <despliegue>"),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runLocalCommand(cmd, newReleaser, func(ctx context.Context, r releaser, views *console.Views) error {
			release, err := r.Release(ctx, args[0], args[1], releaseName)
			if err != nil {
				return err
			}
			views.Released(release)
			return nil
		})
	},
}

var protectCmd = &cobra.Command{
	Use:   "protect <ambiente>",
	Short: "Evita que el ambiente lance solo: los lanzamientos los decides tú",
	Long: `Protege un ambiente (por ejemplo prod): sus despliegues dejan de lanzarse solos y tú eliges cuál queda visible
con "vex release <ambiente> <despliegue>".

Ojo: proteger NO impide desplegar ni hacer rollback; solo el lanzamiento automático.`,
	Example: "  vex protect prod",
	Args:    exactArgs(1, "indica el ambiente: vex protect <ambiente>"),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runLocalCommand(cmd, newReleaser, func(ctx context.Context, r releaser, views *console.Views) error {
			if err := r.Protect(ctx, args[0]); err != nil {
				return err
			}
			views.Protected(args[0])
			return nil
		})
	},
}

var unprotectCmd = &cobra.Command{
	Use:     "unprotect <ambiente>",
	Short:   "Devuelve el ambiente al lanzamiento automático",
	Long:    `Quita la protección de un ambiente: cada despliegue exitoso vuelve a lanzarse solo.`,
	Example: "  vex unprotect prod",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runLocalCommand(cmd, newReleaser, func(ctx context.Context, r releaser, views *console.Views) error {
			if err := r.Unprotect(ctx, args[0]); err != nil {
				return err
			}
			views.Unprotected(args[0])
			return nil
		})
	},
}

func init() {
	releaseCmd.Flags().StringVar(&releaseName, "name", "", "Nombre de la versión (por defecto, su número)")

	vexCmd.AddCommand(releaseCmd, protectCmd, unprotectCmd)
}
