package execution

import (
	"github.com/jairoprogramador/vex/internal/domain/execution/vos"
)

// VarEntryDTO es el objeto de transferencia de datos para la entidad VarEntry.
// Tiene campos exportados para que el paquete gob pueda serializarlos.
type VarDTO struct {
	Name  string
	Value string
}

// toVarsDTO convierte un objeto de valor de dominio VariableSet a su DTO para persistencia.
func toVarsDTO(varSets vos.VariableSet) []VarDTO {
	if varSets == nil {
		return []VarDTO{}
	}
	dtoVars := make([]VarDTO, 0, len(varSets))
	for _, outputVar := range varSets {
		dtoVars = append(dtoVars, VarDTO{
			Name:  outputVar.Name(),
			Value: outputVar.Value(),
		})
	}
	return dtoVars
}

// fromVarsDTO convierte un DTO de persistencia a un objeto de valor de dominio VariableSet.
func fromVarsDTO(varsDto []VarDTO) (vos.VariableSet, error) {
	if varsDto == nil {
		return vos.NewVariableSet(), nil
	}
	vars := make(vos.VariableSet, len(varsDto))
	for _, dtoEntry := range varsDto {
		outputVar, err := vos.NewOutputVar(dtoEntry.Name, dtoEntry.Value, false)
		if err != nil {
			return vos.NewVariableSet(), err
		}
		vars.Add(outputVar)
	}
	return vars, nil
}
