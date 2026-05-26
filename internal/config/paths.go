package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func UserConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".vex", "config"), nil
}

// GlobalConfigPath retorna la ruta del archivo de configuración de sistema
// (aplica a todos los usuarios de la máquina). La ruta varía por plataforma:
//
//   - Linux:   /etc/vex/config
//   - macOS:   /usr/local/etc/vex/config  (convención Homebrew para CLI tools)
//   - Windows: %PROGRAMDATA%\Vex\config   (default: C:\ProgramData\Vex\config)
//
// Escribir en la ruta global puede requerir permisos de administrador/root.
func GlobalConfigPath() string {
	switch runtime.GOOS {
	case "darwin":
		return "/usr/local/etc/vex/config"
	case "windows":
		pd := os.Getenv("PROGRAMDATA")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		return filepath.Join(pd, "Vex", "config")
	default: // linux y otros POSIX
		return "/etc/vex/config"
	}
}
