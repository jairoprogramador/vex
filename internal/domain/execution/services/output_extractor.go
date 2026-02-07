package services

import (
	"fmt"
	"regexp"

	"github.com/jairoprogramador/vex/internal/domain/execution/ports"
	"github.com/jairoprogramador/vex/internal/domain/execution/vos"
)

var (
	defaultOutputExtractor ports.OutputExtractor = &OutputExtractor{}
)

type OutputExtractor struct{}

func NewOutputExtractor() ports.OutputExtractor {
	return defaultOutputExtractor
}

func (oe *OutputExtractor) ExtractVars(commandOutput string, outputs []vos.CommandOutput) (vos.VariableSet, error) {
	extractedVars := vos.NewVariableSet()

	for _, output := range outputs {
		re, err := regexp.Compile(output.Probe())
		if err != nil {
			return nil, fmt.Errorf("expresión regular inválida para la salida '%s': %w", output.Name(), err)
		}

		matches := re.FindStringSubmatch(commandOutput)

		if output.Name() != "" {
			if len(matches) < 2 {
				return nil, fmt.Errorf("no se encontró la variable de salida '%s' en la salida '%s' del comando. Sonda utilizada: %s", output.Name(), commandOutput, output.Probe())
			}

			if matches[1] == "" {
				return nil, fmt.Errorf("la variable de salida '%s' extrajo un valor vacío. Sonda utilizada: %s", output.Name(), output.Probe())
			}
			outputVar, err := vos.NewOutputVar(output.Name(), matches[1], false)
			if err != nil {
				return nil, fmt.Errorf("falló al crear la variable de salida '%s': %w", output.Name(), err)
			}
			extractedVars.Add(outputVar)
		}
	}

	return extractedVars, nil
}
