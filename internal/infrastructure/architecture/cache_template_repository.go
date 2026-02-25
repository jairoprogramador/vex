package architecture

import (
	"fmt"

	"github.com/jairoprogramador/vex-client/internal/domain/architecture/ports"
	"github.com/jairoprogramador/vex-client/internal/domain/architecture/vos"
	proVos "github.com/jairoprogramador/vex-client/internal/domain/project/vos"
)

type CacheTemplateRepository struct {
}

func NewCacheTemplateRepository() ports.TemplateRepository {
	return &CacheTemplateRepository{}
}

func (r *CacheTemplateRepository) GetTemplates(level vos.Level, response []int) (string, error) {
	fmt.Println("GetTemplates", level, response)
	return proVos.DefaultTemplateUrl, nil
}
