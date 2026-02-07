package services_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jairoprogramador/vex/internal/domain/execution/services"
	"github.com/jairoprogramador/vex/internal/domain/execution/vos"
	"github.com/stretchr/testify/assert"
)

// mockInterpolator simula el servicio de interpolación para las pruebas.
type mockInterpolatorVariableResolver struct{}

func (m *mockInterpolatorVariableResolver) Interpolate(input string, vars vos.VariableSet) (string, error) {
	result := input
	for k, v := range vars {
		placeholder := fmt.Sprintf("${var.%s}", k)
		if strings.Contains(result, placeholder) {
			result = strings.ReplaceAll(result, placeholder, v.Value())
		}
	}
	// Si todavía hay un placeholder, significa que falta una variable.
	if strings.Contains(result, "${var.") {
		return "", fmt.Errorf("variable no encontrada")
	}
	return result, nil
}

func TestVariableResolver_Resolve(t *testing.T) {
	interpolator := &mockInterpolatorVariableResolver{}
	resolver := services.NewVariableResolver(interpolator)

	// Helper para crear OutputVar de forma segura en tests
	newVar := func(name, value string) vos.OutputVar {
		v, err := vos.NewOutputVar(name, value, false)
		if err != nil {
			panic(err)
		}
		return v
	}

	testCases := []struct {
		name          string
		initialVars   vos.VariableSet
		varsToResolve vos.VariableSet
		expectedVars  vos.VariableSet
		expectError   bool
		errorContains string
	}{
		{
			name: "deberia_resolver_variables_simples",
			initialVars: vos.VariableSet{
				"project_name": newVar("project_name", "vex"),
			},
			varsToResolve: vos.VariableSet{
				"welcome_message": newVar("welcome_message", "Hello, ${var.project_name}!"),
			},
			expectedVars: vos.VariableSet{
				"welcome_message": newVar("welcome_message", "Hello, vex!"),
			},
			expectError: false,
		},
		{
			name:        "no_deberia_hacer_nada_si_no_hay_nada_que_resolver",
			initialVars: vos.VariableSet{"a": newVar("a", "1")},
			varsToResolve: vos.VariableSet{
				"b": newVar("b", "static_value"),
				"c": newVar("c", "another_static"),
			},
			expectedVars: vos.VariableSet{
				"b": newVar("b", "static_value"),
				"c": newVar("c", "another_static"),
			},
			expectError: false,
		},
		{
			name: "deberia_resolver_dependencias_encadenadas",
			initialVars: vos.VariableSet{
				"base_url": newVar("base_url", "api.example.com"),
			},
			varsToResolve: vos.VariableSet{
				"endpoint":   newVar("endpoint", "/users"),
				"full_url":   newVar("full_url", "https://${var.base_url}${var.endpoint}"),
				"health_url": newVar("health_url", "${var.full_url}/health"),
			},
			expectedVars: vos.VariableSet{
				"endpoint":   newVar("endpoint", "/users"),
				"full_url":   newVar("full_url", "https://api.example.com/users"),
				"health_url": newVar("health_url", "https://api.example.com/users/health"),
			},
			expectError: false,
		},
		{
			name:          "deberia_manejar_un_conjunto_vacio_de_variables_a_resolver",
			initialVars:   vos.VariableSet{"a": newVar("a", "1")},
			varsToResolve: vos.NewVariableSet(),
			expectedVars:  vos.NewVariableSet(),
			expectError:   false,
		},
		{
			name:        "deberia_fallar_con_dependencia_circular",
			initialVars: vos.NewVariableSet(),
			varsToResolve: vos.VariableSet{
				"a": newVar("a", "val-a-${var.b}"),
				"b": newVar("b", "val-b-${var.a}"),
			},
			expectError:   true,
			errorContains: "dependencia circular o variable faltante",
		},
		{
			name:        "deberia_fallar_si_falta_una_variable",
			initialVars: vos.NewVariableSet(),
			varsToResolve: vos.VariableSet{
				"a": newVar("a", "val-a-${var.missing}"),
			},
			expectError:   true,
			errorContains: "dependencia circular o variable faltante",
		},
		{
			name:        "deberia_resolver_dependencias_dentro_del_mismo_conjunto",
			initialVars: vos.NewVariableSet(),
			varsToResolve: vos.VariableSet{
				"b": newVar("b", "${var.a}-world"),
				"a": newVar("a", "hello"),
				"c": newVar("c", "${var.b}-c"),
			},
			expectedVars: vos.VariableSet{
				"a": newVar("a", "hello"),
				"b": newVar("b", "hello-world"),
				"c": newVar("c", "hello-world-c"),
			},
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resolved, err := resolver.Resolve(tc.initialVars, tc.varsToResolve)

			if tc.expectError {
				assert.Error(t, err)
				if tc.errorContains != "" {
					assert.Contains(t, err.Error(), tc.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.True(t, tc.expectedVars.Equals(resolved), "Expected:\n%v\nGot:\n%v", tc.expectedVars, resolved)
			}
		})
	}
}
