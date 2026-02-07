package state

import (
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/jairoprogramador/vex/internal/domain/state/aggregates"
	"github.com/jairoprogramador/vex/internal/domain/state/ports"
)

type GobStateRepository struct{}

func NewGobStateRepository() ports.StateRepository {
	return &GobStateRepository{}
}

func (r *GobStateRepository) Get(filePath string) (*aggregates.StateTable, error) {
	file, err := os.Open(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Si el archivo no existe, no es un error. Simplemente no hay estado.
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	var stateTableDTO StateTableDTO
	decoder := gob.NewDecoder(file)

	if err := decoder.Decode(&stateTableDTO); err != nil {
		if err == io.EOF {
			// Un archivo vacío significa que no hay estado, no es un error fatal.
			return nil, nil
		}
		return nil, fmt.Errorf("error al decodificar el estado: %w", err)
	}

	return fromDTO(&stateTableDTO), nil
}

func (r *GobStateRepository) Save(filePath string, stateTable *aggregates.StateTable) error {
	if stateTable == nil {
		return errors.New("no se puede guardar una tabla de estado nula")
	}

	// Mapear del agregado de dominio al DTO
	dto := toStateTableDTO(stateTable)

	// Asegurarse de que el directorio exista
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := gob.NewEncoder(file)
	if err := encoder.Encode(dto); err != nil {
		return fmt.Errorf("error al codificar el estado: %w", err)
	}

	return nil
}
