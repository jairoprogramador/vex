package vos

import (
	comVos "github.com/jairoprogramador/vex/internal/domain/common/vos"
)

type ExecutionUnit struct {
	image    comVos.Image
	template comVos.Template
}

func NewExecutionUnit(image comVos.Image, template comVos.Template) ExecutionUnit {
	return ExecutionUnit{image: image, template: template}
}

func (e ExecutionUnit) Image() comVos.Image       { return e.image }
func (e ExecutionUnit) Template() comVos.Template { return e.template }
