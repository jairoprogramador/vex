// Package journal guarda en este equipo la constancia de lo que se aceptó aplicar en la nube con `vex bootstrap`
// y de cada desconexión: quién, sobre qué suscripción, qué commit y qué huella, y qué permisos resaltados se
// aceptaron. No guarda secretos.
package journal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jairoprogramador/vex/internal/application"
)

// File añade cada constancia como una línea JSON a un archivo que solo lee su dueño.
type File struct {
	path string
}

func NewFile(path string) *File { return &File{path: path} }

var _ application.BootstrapJournal = (*File)(nil)

func (f *File) Record(record application.AcceptanceRecord) error {
	line, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("serializar la constancia: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return fmt.Errorf("crear %s: %w", filepath.Dir(f.path), err)
	}
	file, err := os.OpenFile(f.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("abrir %s: %w", f.path, err)
	}
	if _, err := file.Write(append(line, '\n')); err != nil {
		file.Close()
		return fmt.Errorf("escribir %s: %w", f.path, err)
	}
	return file.Close()
}
