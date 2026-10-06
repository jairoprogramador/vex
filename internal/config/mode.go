package config

type ExecutionMode string

const (
	ModeRemote ExecutionMode = "remote"
	ModeLocal  ExecutionMode = "local"
	// ModeHybrid ExecutionMode = "hybrid"  // reservado para uso futuro

	// ModeUnset es el valor cero; indica que el scope no tiene configuración.
	// LoadEffective nunca retorna ModeUnset — usa DefaultMode como default final.
	ModeUnset ExecutionMode = ""
)

// DefaultMode es el modo que rige cuando ningún nivel de configuración define uno.
const DefaultMode = ModeLocal

func (m ExecutionMode) IsValid() bool {
	return m == ModeRemote || m == ModeLocal
}

func (m ExecutionMode) String() string { return string(m) }
