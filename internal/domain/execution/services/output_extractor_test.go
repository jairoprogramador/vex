package services_test

import (
	"testing"

	"github.com/jairoprogramador/vex/internal/domain/execution/services"
	"github.com/jairoprogramador/vex/internal/domain/execution/vos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newOutputCommandForTest(name, probe string) vos.CommandOutput {
	cmd, _ := vos.NewCommandOutput(name, probe)
	return cmd
}

func TestOutputExtractor_Extract(t *testing.T) {

	testCases := []struct {
		name          string
		outputs       []vos.CommandOutput
		commandOutput string
		expectedVars  vos.VariableSet
		expectError   bool
	}{
		{
			name:          "Extraccion Exitosa Simple",
			outputs:       []vos.CommandOutput{newOutputCommandForTest("azure_cr", `azure_cr\s*=\s*"([^"]+)"`)},
			commandOutput: `azure_cr = "valor"`,
			expectedVars:  vos.NewVariableSetFromMap(map[string]string{"azure_cr": "valor"}),
			expectError:   false,
		},
		{
			name: "Multiples Extracciones Exitosas",
			outputs: []vos.CommandOutput{
				newOutputCommandForTest("user", `user:\s+(\w+)`),
				newOutputCommandForTest("id", `id:\s+(\d+)`),
			},
			commandOutput: "user: myuser, id: 12345",
			expectedVars:  vos.NewVariableSetFromMap(map[string]string{"user": "myuser", "id": "12345"}),
			expectError:   false,
		},
		{
			name:          "Error - Sin Coincidencia (No Match)",
			outputs:       []vos.CommandOutput{newOutputCommandForTest("token", `token=(.+)`)},
			commandOutput: "no token found in this output",
			expectedVars:  nil,
			expectError:   true,
		},
		{
			name:          "Error - Expresion Regular Invalida",
			outputs:       []vos.CommandOutput{newOutputCommandForTest("bad_regex", `(`)},
			commandOutput: "some output",
			expectedVars:  nil,
			expectError:   true,
		},
		{
			name:          "Error - Sin Grupo de Captura",
			outputs:       []vos.CommandOutput{newOutputCommandForTest("version", `go1.18.3`)}, // Regex sin (...)
			commandOutput: "go version go1.18.3 linux/amd64",
			expectedVars:  nil,
			expectError:   true,
		},
		{
			name:          "Sin OutputCommands definidos",
			outputs:       []vos.CommandOutput{},
			commandOutput: "some random output",
			expectedVars:  vos.NewVariableSet(),
			expectError:   false,
		},
		{
			name:          "Salida de Comando Vacia",
			outputs:       []vos.CommandOutput{newOutputCommandForTest("anything", `(.*)`)},
			commandOutput: "",
			expectedVars:  nil,
			expectError:   true,
		},
	}

	extractor := services.NewOutputExtractor()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := extractor.ExtractVars(tc.commandOutput, tc.outputs)

			if tc.expectError {
				require.Error(t, err, "Se esperaba un error pero no se obtuvo")
			} else {
				require.NoError(t, err, "No se esperaba un error pero se obtuvo uno")
				assert.Equal(t, tc.expectedVars, result, "El mapa de variables extraídas no es el esperado")
			}
		})
	}
}
