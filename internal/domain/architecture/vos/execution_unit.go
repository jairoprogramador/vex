package vos

import (
	comVos "github.com/jairoprogramador/vex/internal/domain/common/vos"
)

type ExecutionUnit struct {
	image    comVos.Image
	template comVos.Pipeline
	contract string
}

func NewExecutionUnit(image comVos.Image, template comVos.Pipeline) ExecutionUnit {
	return ExecutionUnit{image: image, template: template}
}

func (e ExecutionUnit) Image() comVos.Image       { return e.image }
func (e ExecutionUnit) Template() comVos.Pipeline { return e.template }

// WithContract devuelve la unidad con el contrato de nube que declara su plantilla en el catálogo.
func (e ExecutionUnit) WithContract(contract string) ExecutionUnit {
	e.contract = contract
	return e
}

// Contract es el contrato de nube de la plantilla ("azure/v1"); vacío si no declara ninguno.
func (e ExecutionUnit) Contract() string { return e.contract }
