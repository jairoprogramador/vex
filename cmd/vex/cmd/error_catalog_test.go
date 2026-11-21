package cmd

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jairoprogramador/vex/internal/application"
	"github.com/jairoprogramador/vex/internal/infrastructure/factories"
)

// vexCommand encuentra «vex <comando> [<ambiente>] [--opción]» en un texto de ayuda.
var vexCommand = regexp.MustCompile("`vex ([a-z]+)((?: [^`]*)?)`")

// Lo que un error aconseja ejecutar tiene que existir: un consejo con un comando inventado es peor que ninguno.
func TestCatalogo_LosComandosQueAconsejaExisten(t *testing.T) {
	var advice []string
	for _, rule := range application.ErrorRules(factories.CloudErrorCatalogs()...) {
		message, action, ok := rule.Explain(rule.Example)
		require.True(t, ok, rule.Code)
		advice = append(advice, message, action)
	}
	for _, hint := range factories.CloudErrorHints() {
		advice = append(advice, hint.Message)
	}
	advice = append(advice, application.UnsignedTemplateNotice)

	checked := 0
	for _, text := range advice {
		for _, match := range vexCommand.FindAllStringSubmatch(text, -1) {
			name, rest := match[1], match[2]
			command, _, err := vexCmd.Find([]string{name})
			require.NoError(t, err)
			require.NotSame(t, vexCmd, command, "«vex %s» no es un comando (en: %s)", name, text)
			for _, flag := range regexp.MustCompile(`--[a-z-]+`).FindAllString(rest, -1) {
				assert.NotNil(t, command.Flags().Lookup(strings.TrimPrefix(flag, "--")), "«vex %s» no tiene %s (en: %s)", name, flag, text)
			}
			checked++
		}
	}
	assert.Greater(t, checked, 10, "el catálogo aconseja comandos de vex")
}

// Los códigos de los adaptadores y de sus pistas también están en la documentación.
func TestCatalogo_LosCodigosDeLasNubesEstanDocumentados(t *testing.T) {
	doc, err := os.ReadFile("../../../docs/solucion-de-problemas.md")
	require.NoError(t, err)
	for _, rule := range application.ErrorRules(factories.CloudErrorCatalogs()...) {
		assert.True(t, strings.Contains(string(doc), "`"+rule.Code+"`"), "el código %s no está en docs/solucion-de-problemas.md", rule.Code)
	}
	for _, hint := range factories.CloudErrorHints() {
		require.NotEmpty(t, hint.Code, "toda pista tiene código: %s", hint.Message)
		assert.True(t, strings.Contains(string(doc), "`"+hint.Code+"`"), "el código %s no está en docs/solucion-de-problemas.md", hint.Code)
	}
}
