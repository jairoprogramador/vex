package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/jairoprogramador/vex/internal/config"
	"github.com/spf13/cobra"
)

var scopeFlag string

// configCmd es el comando raíz de `vex config`.
var configCmd = &cobra.Command{
	Use:   "config [key | key=value]",
	Short: "Gestionar la configuración de la CLI de vex",
	Long: `Lee y escribe la configuración de vex en tres niveles de prioridad:

  project  →  vexconfig.yaml en el directorio actual (mayor prioridad)
  user     →  [user]/.vex/config
  global   →  ruta de sistema (/etc/vex/config en Linux,
               /usr/local/etc/vex/config en macOS,
               %%PROGRAMDATA%%\Vex\config en Windows)

El nivel de proyecto tiene mayor prioridad; el global, menor.
Si ningún nivel define un valor, se usa el default (modo: remote).

Ejemplos:
  vex config mode              # muestra el valor efectivo de mode
  vex config mode=remote       # establece mode en el scope project
  vex config mode=local --scope user   # establece mode en scope user
  vex config unset mode        # borra mode del scope project
  vex config list              # lista todos los scopes`,
	Args: cobra.MaximumNArgs(1),
	RunE: runConfig,
}

var configUnsetCmd = &cobra.Command{
	Use:       "unset <key>",
	Short:     "Borra una clave de configuración del scope indicado",
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"mode"},
	RunE:      runConfigUnset,
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "Lista todas las claves de configuración y sus valores",
	Args:  cobra.NoArgs,
	RunE:  runConfigList,
}

func init() {
	// --scope es un persistent flag heredado por todos los subcomandos.
	configCmd.PersistentFlags().StringVar(&scopeFlag, "scope", "",
		`Nivel de configuración: "project" (vexconfig.yaml), "user" ([user]/.vex/config),
"global" (ruta de sistema). Default para 'unset': "project".
Default para 'key' / 'list': muestra el valor efectivo (fusionado).`)

	configCmd.AddCommand(configUnsetCmd)
	configCmd.AddCommand(configListCmd)
}

// --- handlers ---------------------------------------------------------------

func runConfig(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	arg := args[0]
	key, val, found := strings.Cut(arg, "=")
	if !found {
		return configGet(cmd, key)
	}
	if val == "" {
		return fmt.Errorf("valor vacío: para borrar una clave usa 'vex config unset %s'", key)
	}
	return configSet(cmd, key, val)
}

func configGet(cmd *cobra.Command, key string) error {
	if err := validateKey(key); err != nil {
		return err
	}

	projectPath, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve project path: %w", err)
	}

	if scopeFlag == "" {
		effective, err := config.LoadEffective(projectPath)
		if err != nil {
			return err
		}
		origin, err := findOrigin(key, projectPath)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s = %s  (from: %s)\n", key, effective.Mode, origin)
		return nil
	}

	scope, err := parseScope(scopeFlag)
	if err != nil {
		return err
	}
	cfg, err := config.LoadScope(scope, projectPath)
	if err != nil {
		return err
	}
	val := valueFor(key, cfg)
	if val == "" {
		val = "(sin definir)"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s = %s\n", key, val)
	return nil
}

func configSet(cmd *cobra.Command, key, rawVal string) error {
	if err := validateKey(key); err != nil {
		return err
	}

	scope := config.ScopeProject
	if scopeFlag != "" {
		s, err := parseScope(scopeFlag)
		if err != nil {
			return err
		}
		scope = s
	}

	projectPath, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve project path: %w", err)
	}

	cfg, err := buildConfig(key, rawVal)
	if err != nil {
		return err
	}

	if err := config.SaveScope(scope, cfg, projectPath); err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("%w\n(sugerencia: para scope global usa sudo o --scope user)", err)
		}
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%s = %s  (scope: %s)\n", key, rawVal, scope)
	return nil
}

func runConfigUnset(cmd *cobra.Command, args []string) error {
	key := args[0]
	if err := validateKey(key); err != nil {
		return err
	}

	scope := config.ScopeProject
	if scopeFlag != "" {
		s, err := parseScope(scopeFlag)
		if err != nil {
			return err
		}
		scope = s
	}

	projectPath, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve project path: %w", err)
	}

	// Guardar un Config vacío borra el campo gracias a omitempty.
	if err := config.SaveScope(scope, config.Config{}, projectPath); err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("%w\n(sugerencia: para scope global usa sudo o --scope user)", err)
		}
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%s eliminado del scope %s\n", key, scope)
	return nil
}

func runConfigList(cmd *cobra.Command, args []string) error {
	projectPath, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve project path: %w", err)
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	defer w.Flush()

	if scopeFlag != "" {
		scope, err := parseScope(scopeFlag)
		if err != nil {
			return err
		}
		cfg, err := config.LoadScope(scope, projectPath)
		if err != nil {
			return err
		}
		printScopeRow(w, scope, cfg)
		return nil
	}

	effective, err := config.LoadEffective(projectPath)
	if err != nil {
		return err
	}
	origin, err := findOrigin("mode", projectPath)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Efectivo\tmode\t%s\t(desde: %s)\n\n", effective.Mode, origin)
	fmt.Fprintln(w, "Scope\tKey\tValue")

	for _, scope := range []config.Scope{config.ScopeGlobal, config.ScopeUser, config.ScopeProject} {
		cfg, err := config.LoadScope(scope, projectPath)
		if err != nil {
			return err
		}
		printScopeRow(w, scope, cfg)
	}
	return nil
}

// --- helpers ----------------------------------------------------------------

func validateKey(key string) error {
	switch key {
	case "mode":
		return nil
	default:
		return fmt.Errorf("clave desconocida %q (claves válidas: mode)", key)
	}
}

func parseScope(s string) (config.Scope, error) {
	switch config.Scope(s) {
	case config.ScopeGlobal, config.ScopeUser, config.ScopeProject:
		return config.Scope(s), nil
	default:
		return "", fmt.Errorf("scope %q inválido: debe ser %q, %q o %q",
			s, config.ScopeGlobal, config.ScopeUser, config.ScopeProject)
	}
}

// buildConfig construye un Config con el valor validado para la clave dada.
func buildConfig(key, rawVal string) (config.Config, error) {
	switch key {
	case "mode":
		m := config.ExecutionMode(rawVal)
		if !m.IsValid() {
			return config.Config{}, fmt.Errorf(
				"valor %q inválido para mode: debe ser %q o %q",
				rawVal, config.ModeRemote, config.ModeLocal)
		}
		return config.Config{Mode: m}, nil
	default:
		return config.Config{}, fmt.Errorf("clave desconocida: %q", key)
	}
}

// valueFor retorna el valor de key dentro de cfg como string.
func valueFor(key string, cfg config.Config) string {
	switch key {
	case "mode":
		return string(cfg.Mode)
	default:
		return ""
	}
}

// findOrigin retorna el nombre del scope que aporta el valor efectivo de key.
func findOrigin(key, projectPath string) (string, error) {
	for _, scope := range []config.Scope{config.ScopeProject, config.ScopeUser, config.ScopeGlobal} {
		cfg, err := config.LoadScope(scope, projectPath)
		if err != nil {
			return "", err
		}
		if valueFor(key, cfg) != "" {
			return string(scope), nil
		}
	}
	return "default", nil
}

func printScopeRow(w *tabwriter.Writer, scope config.Scope, cfg config.Config) {
	val := string(cfg.Mode)
	if val == "" {
		val = "(sin definir)"
	}
	fmt.Fprintf(w, "%s\tmode\t%s\n", scope, val)
}
