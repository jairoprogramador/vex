package architecture

import (
	"github.com/jairoprogramador/vex/internal/domain/architecture/ports"
	"github.com/jairoprogramador/vex/internal/domain/architecture/vos"
)

type CacheLevelRepository struct {
	levels []vos.Level
}

func NewCacheLevelRepository() ports.LevelRepository {
	levels := []vos.Level{
		vos.NewLevel("Básico", 0),
		vos.NewLevel("Comercial", 1),
		vos.NewLevel("Crítico", 2),
	}
	return &CacheLevelRepository{levels: levels}
}

func (r *CacheLevelRepository) GetLevels() ([]vos.Level, error) {
	return r.levels, nil
}
