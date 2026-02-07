package ports

import (
	"github.com/jairoprogramador/vex/internal/domain/state/vos"
)

// StateManager define la interfaz para gestionar el estado de los pasos.
type StateManager interface {
	// HasStateChanged comprueba si el estado actual de un paso ha cambiado
	// en comparación con el estado guardado, según una política de caché.
	HasStateChanged(
		stateTablePath string,
		currentState vos.CurrentStateFingerprints,
		policy vos.CachePolicy,
	) (bool, error)

	// UpdateState guarda el nuevo estado de un paso.
	UpdateState(stateTablePath string, currentState vos.CurrentStateFingerprints) error
}
