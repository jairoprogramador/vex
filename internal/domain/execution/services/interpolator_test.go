package services_test

import (
	"testing"

	"github.com/jairoprogramador/vex/internal/domain/execution/services"
	"github.com/jairoprogramador/vex/internal/domain/execution/vos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newVarsFromMap(m map[string]string) vos.VariableSet {
	vs := vos.NewVariableSet()
	for k, v := range m {
		ov, err := vos.NewOutputVar(k, v, false)
		if err != nil {
			panic(err)
		}
		vs.Add(ov)
	}
	return vs
}

func TestInterpolator_Interpolate(t *testing.T) {
	testCases := []struct {
		name           string
		input          string
		vars           vos.VariableSet
		expectedOutput string
		expectError    bool
	}{
		{
			name:           "Test Basico Exitoso",
			input:          "Hola, ${var.nombre}!",
			vars:           newVarsFromMap(map[string]string{"nombre": "Mundo"}),
			expectedOutput: "Hola, Mundo!",
			expectError:    false,
		},
		{
			name:           "Multiples Variables",
			input:          "El valor de ${var.uno} es 1 y el de ${var.dos} es 2.",
			vars:           newVarsFromMap(map[string]string{"uno": "ONE", "dos": "TWO"}),
			expectedOutput: "El valor de ONE es 1 y el de TWO es 2.",
			expectError:    false,
		},
		{
			name:        "Variable Faltante",
			input:       "Hola, ${var.nombre}. ¿Cómo estás?",
			vars:        newVarsFromMap(map[string]string{"otro": "valor"}),
			expectError: true,
		},
		{
			name:           "Sin Variables en Input",
			input:          "Esta cadena no tiene variables.",
			vars:           newVarsFromMap(map[string]string{"nombre": "Mundo"}),
			expectedOutput: "Esta cadena no tiene variables.",
			expectError:    false,
		},
		{
			name:           "Input Vacio",
			input:          "",
			vars:           newVarsFromMap(map[string]string{"nombre": "Mundo"}),
			expectedOutput: "",
			expectError:    false,
		},
		{
			name:        "Error de Interpolacion Malformada",
			input:       "Esto tiene una variable ${var.malformada}.",
			vars:        vos.NewVariableSet(),
			expectError: true,
		},
		{
			name:           "Variable al Inicio y al Final",
			input:          "${var.saludo}, te despides con ${var.despedida}",
			vars:           newVarsFromMap(map[string]string{"saludo": "Hola", "despedida": "Adiós"}),
			expectedOutput: "Hola, te despides con Adiós",
			expectError:    false,
		},
		{
			name:        "Mapa de Variables Vacio",
			input:       "El valor es ${var.valor}",
			vars:        vos.NewVariableSet(),
			expectError: true,
		},
	}

	interpolator := services.NewInterpolator()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := interpolator.Interpolate(tc.input, tc.vars)

			if tc.expectError {
				require.Error(t, err, "Se esperaba un error pero no se obtuvo")
			} else {
				require.NoError(t, err, "No se esperaba un error pero se obtuvo uno")
				assert.Equal(t, tc.expectedOutput, result, "El resultado de la interpolación no es el esperado")
			}
		})
	}
}
