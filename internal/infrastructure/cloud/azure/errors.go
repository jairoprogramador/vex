package azure

import (
	"errors"

	"github.com/jairoprogramador/vex/internal/application"
)

var _ application.ErrorCatalog = Adapter{}

// ErrorRules son los errores que solo este adaptador conoce: los de la CLI de Azure. Lo que `az` escribe es dato
// no confiable: de un comando fallido solo se muestra su stderr ya acotado, sin caracteres de control y con los
// tokens tachados (ver CommandError).
func (Adapter) ErrorRules() []application.CatalogRule {
	return []application.CatalogRule{
		application.Fixed("az_no_instalado", ErrNotInstalled,
			"No encontré la CLI de Azure (az).",
			"Instálala (https://aka.ms/installazurecli) y vuelve a ejecutar el comando"),
		application.Fixed("az_sin_sesion", ErrNoSession,
			"No hay una sesión de Azure en este equipo.",
			"Ejecuta `az login` y vuelve a ejecutar el comando"),
		{
			Code: "az_fallo", Example: &CommandError{Command: "deployment sub create", Stderr: "detalle"},
			Explain: func(err error) (string, string, bool) {
				var failed *CommandError
				if !errors.As(err, &failed) {
					return "", "", false
				}
				return err.Error(), "No se continuó. Revisa el detalle; `az " + failed.Command + " --help` describe el comando, " +
					"y `vex bootstrap <ambiente> --check` dice cómo quedó la conexión", true
			},
		},
	}
}
