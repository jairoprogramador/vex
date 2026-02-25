package ports

import "github.com/jairoprogramador/vex-client/internal/domain/architecture/vos"

type LevelRepository interface {
	GetLevels() ([]vos.Level, error)
}
