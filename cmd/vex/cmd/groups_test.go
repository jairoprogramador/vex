package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRoot(names ...string) *cobra.Command {
	root := &cobra.Command{Use: "vex", RunE: func(*cobra.Command, []string) error { return nil }}
	for _, name := range names {
		root.AddCommand(&cobra.Command{Use: name, Short: "hace " + name, Run: func(*cobra.Command, []string) {}})
	}
	return root
}

func helpOf(t *testing.T, root *cobra.Command) string {
	t.Helper()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--help"})
	require.NoError(t, root.Execute(), "una ayuda con grupos mal definidos falla al ejecutarse")
	return out.String()
}

func TestApplyGroups_LaAyudaSeAgrupaPorEncabezados(t *testing.T) {
	root := newTestRoot("init", "run", "config")

	applyGroups(root)

	help := helpOf(t, root)
	assert.Contains(t, help, "Empezar:\n  init")
	assert.Contains(t, help, "Ejecutar:\n  run")
	assert.Contains(t, help, "Configuración:\n  config")
}

func TestApplyGroups_NoMuestraEncabezadosVacios(t *testing.T) {
	root := newTestRoot("init")

	applyGroups(root)

	help := helpOf(t, root)
	assert.Contains(t, help, "Empezar:")
	assert.NotContains(t, help, "Consultar:")
	assert.NotContains(t, help, "Desplegar y lanzar:")
}

func TestApplyGroups_UnComandoSinGrupoQuedaEnAdicionales(t *testing.T) {
	root := newTestRoot("init", "raro")

	applyGroups(root)

	help := helpOf(t, root)
	additional := strings.Index(help, "Additional Commands:")
	require.GreaterOrEqual(t, additional, 0)
	assert.Greater(t, strings.Index(help, "raro"), additional, "un comando sin grupo no desaparece de la ayuda")
}

func TestGroupOf_TodoComandoApuntaAUnGrupoQueExiste(t *testing.T) {
	known := map[string]bool{}
	for _, g := range groupTitles {
		known[g.id] = true
	}
	for command, group := range groupOf {
		assert.True(t, known[group], "el comando %q apunta al grupo %q, que no está registrado", command, group)
	}
}

func TestRunCmd_ComparteLosFlagsDelAtajo(t *testing.T) {
	assert.NotNil(t, runCmd.Flags().Lookup("mode"))
	assert.NotNil(t, runCmd.Flags().Lookup("no-follow"))
	assert.NoError(t, runCmd.Args(runCmd, []string{"release", "prod"}))
	assert.Error(t, runCmd.Args(runCmd, []string{}), "sin paso no hay qué ejecutar")
	assert.Error(t, runCmd.Args(runCmd, []string{"a", "b", "c"}))
}
