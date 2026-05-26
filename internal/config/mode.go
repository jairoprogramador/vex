package config

type ExecutionMode string

const (
	ModeRemote ExecutionMode = "remote"
	ModeLocal  ExecutionMode = "local"
	// ModeHybrid ExecutionMode = "hybrid"  // reservado para uso futuro

	// ModeUnset es el valor cero; indica que el scope no tiene configuración.
	// LoadEffective nunca retorna ModeUnset — usa ModeRemote como default final.
	ModeUnset ExecutionMode = ""
)

func (m ExecutionMode) IsValid() bool {
	return m == ModeRemote || m == ModeLocal
}

func (m ExecutionMode) String() string { return string(m) }
