package ports

import "github.com/jairoprogramador/vex/internal/domain/execution/vos"

type VarsRepository interface {
	Get(filePath string) (vos.VariableSet, error)
	Save(filePath string, generatedVars vos.VariableSet) error
}
