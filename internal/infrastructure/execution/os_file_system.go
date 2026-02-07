package execution

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jairoprogramador/vex/internal/domain/execution/ports"
)

// OSFileSystem es una implementación de la interfaz FileSystem que utiliza el paquete 'os' de Go.
type OSFileSystem struct{}

// NewOSFileSystem crea una nueva instancia de OSFileSystem.
func NewOSFileSystem() ports.FileSystem {
	return &OSFileSystem{}
}

// ReadFile lee el contenido de un archivo desde la ruta especificada.
func (fs *OSFileSystem) ReadFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error al leer el archivo %s: %w", path, err)
	}
	return data, nil
}

// WriteFile escribe datos en un archivo en la ruta especificada.
// Crea el directorio si no existe.
func (fs *OSFileSystem) WriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("error al crear el directorio %s: %w", dir, err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("error al escribir el archivo %s: %w", path, err)
	}
	return nil
}
