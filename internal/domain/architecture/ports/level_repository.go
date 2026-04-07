package ports

import "github.com/jairoprogramador/vex/internal/domain/architecture/vos"

type LevelRepository interface {
	GetLevels() ([]vos.Level, error)
}
