package execution

import (
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/jairoprogramador/vex/internal/domain/execution/ports"
	"github.com/jairoprogramador/vex/internal/domain/execution/vos"
)

// GobVarsRepository es una implementación de VarsRepository que usa gob para la persistencia.
type GobVarsRepository struct{}

// NewGobVarsRepository crea una nueva instancia de GobVarsRepository.
func NewGobVarsRepository() ports.VarsRepository {
	return &GobVarsRepository{}
}

// Get carga una VarTable desde un archivo.
// Si el archivo no existe o está vacío, devuelve una tabla vacía sin error.
func (r *GobVarsRepository) Get(filePath string) (vos.VariableSet, error) {
	file, err := os.Open(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// El archivo no existe, devolvemos una tabla nueva y vacía.
			return vos.NewVariableSet(), nil
		}
		return nil, fmt.Errorf("no se pudo abrir el archivo de variables '%s': %w", filePath, err)
	}
	defer file.Close()

	var dtos []VarDTO
	decoder := gob.NewDecoder(file)
	if err := decoder.Decode(&dtos); err != nil {
		if errors.Is(err, io.EOF) {
			// El archivo está vacío, devolvemos una tabla nueva y vacía.
			return vos.NewVariableSet(), nil
		}
		return nil, fmt.Errorf("no se pudo decodificar el conjunto de variables desde '%s': %w", filePath, err)
	}

	varSets, err := fromVarsDTO(dtos)
	if err != nil {
		return vos.NewVariableSet(), err
	}

	return varSets, nil
}

// Save guarda una VarTable en un archivo.
func (r *GobVarsRepository) Save(filePath string, varSets vos.VariableSet) error {
	if len(varSets) == 0 {
		return nil
	}

	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("no se pudo crear el directorio para el archivo de conjunto de variables '%s': %w", dir, err)
	}

	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("no se pudo crear el archivo de conjunto de variables '%s': %w", filePath, err)
	}
	defer file.Close()

	dtos := toVarsDTO(varSets)
	encoder := gob.NewEncoder(file)
	if err := encoder.Encode(dtos); err != nil {
		return fmt.Errorf("no se pudo codificar el conjunto de variables a '%s': %w", filePath, err)
	}

	return nil
}
