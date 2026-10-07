package cmd

import "github.com/spf13/cobra"

// Grupos de la ayuda. Un dev junior que escribe `vex --help` ve cinco encabezados con una frase cada uno y no
// una lista alfabética de veinte comandos.
const (
	groupStart   = "start"
	groupRun     = "run"
	groupQuery   = "query"
	groupRelease = "release"
	groupConfig  = "config"
	groupRemote  = "remote"
)

// groupOf asigna cada comando a su grupo por nombre. Está centralizado aquí a propósito: los comandos nuevos se
// añaden en un solo sitio y la ayuda sigue ordenada.
var groupOf = map[string]string{
	"init": groupStart, "arq": groupStart,
	"run": groupRun, "check": groupRun,
	"ls": groupQuery, "show": groupQuery, "log": groupQuery, "why": groupQuery,
	"deployments": groupQuery, "releases": groupQuery, "envs": groupQuery, "steps": groupQuery,
	"rollback": groupRelease, "release": groupRelease, "protect": groupRelease, "unprotect": groupRelease,
	"abandon": groupRelease,
	"config":  groupConfig, "mode": groupConfig, "version": groupConfig,
	"login": groupRemote, "logout": groupRemote, "whoami": groupRemote, "cancel": groupRemote,
}

var groupTitles = []struct{ id, title string }{
	{groupStart, "Empezar:"},
	{groupRun, "Ejecutar:"},
	{groupQuery, "Consultar:"},
	{groupRelease, "Desplegar y lanzar:"},
	{groupConfig, "Configuración:"},
	{groupRemote, "Modo remoto (portal):"},
}

// applyGroups asigna cada comando a su grupo y registra solo los grupos que tienen comandos: así la ayuda no
// muestra encabezados vacíos. Se llama una vez, antes de ejecutar el comando raíz.
func applyGroups(root *cobra.Command) {
	used := map[string]bool{}
	for _, command := range root.Commands() {
		if group, ok := groupOf[command.Name()]; ok {
			command.GroupID = group
			used[group] = true
		}
	}
	for _, g := range groupTitles {
		if used[g.id] {
			root.AddGroup(&cobra.Group{ID: g.id, Title: g.title})
		}
	}
}
