package services

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/jairoprogramador/vex/internal/domain/execution/ports"
	"github.com/jairoprogramador/vex/internal/domain/execution/vos"
)

var (
	defaultInterpolator ports.Interpolator = &Interpolator{}
	// Regex para encontrar placeholders como ${var.nombre_de_variable}
	varRegex = regexp.MustCompile(`\$\{var\.([a-zA-Z0-9_]+)\}`)
)

type Interpolator struct{}

func NewInterpolator() ports.Interpolator {
	return defaultInterpolator
}

func (i *Interpolator) Interpolate(input string, vars vos.VariableSet) (string, error) {
	var firstError error

	replacerFunc := func(placeholder string) string {
		matches := varRegex.FindStringSubmatch(placeholder)
		if len(matches) < 2 {
			if firstError == nil {
				firstError = fmt.Errorf("placeholder mal formado encontrado: %s", placeholder)
			}
			return placeholder
		}
		varName := matches[1]

		val, exists := vars.Get(varName)
		if !exists {
			if firstError == nil {
				firstError = fmt.Errorf("variable '%s' no encontrada para interpolación", varName)
			}
			return placeholder
		}
		return val.Value()
	}

	result := varRegex.ReplaceAllStringFunc(input, replacerFunc)

	if firstError != nil {
		return "", firstError
	}

	if strings.Contains(result, "${var.") {
		return "", fmt.Errorf("interpolación incompleta, es posible que haya placeholders mal formados. Resultado: %s", result)
	}

	return result, nil
}
