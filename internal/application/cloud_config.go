package application

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jairoprogramador/vex/internal/domain/project/aggregates"
	proVos "github.com/jairoprogramador/vex/internal/domain/project/vos"
)

// ErrCloudConfig indica que vexconfig.yaml mezcla las credenciales del portal con variables reservadas.
var ErrCloudConfig = errors.New("configuración de nube inválida")

// ValidateCloudConfig impide que runtime.run.envs pise las variables que entrega el portal. reserved son los
// prefijos que el usuario no puede definir cuando cloud.credentials es "portal".
func ValidateCloudConfig(project *aggregates.Project, reserved []string) error {
	conflicts := project.Cloud().ConflictingEnvNames(project.Runtime().Env(), reserved)
	if len(conflicts) == 0 {
		return nil
	}
	return fmt.Errorf("%w: cloud.credentials es %q pero runtime.run.envs define %s.\n→ "+
		"Quita esas variables de vexconfig.yaml, o cambia a \"credentials: %s\" si quieres usar tus propios secretos",
		ErrCloudConfig, proVos.CredentialsPortal, strings.Join(conflicts, ", "), proVos.CredentialsEnv)
}
