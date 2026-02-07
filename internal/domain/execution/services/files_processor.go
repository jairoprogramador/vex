package services

import (
	"fmt"

	"github.com/jairoprogramador/vex/internal/domain/execution/ports"
	"github.com/jairoprogramador/vex/internal/domain/execution/vos"
)

type FileProcessor struct {
	backups      map[string][]byte
	fs           ports.FileSystem
	interpolator ports.Interpolator
}

func NewFileProcessor(fs ports.FileSystem, interpolator ports.Interpolator) ports.FileProcessor {
	return &FileProcessor{
		backups:      make(map[string][]byte),
		fs:           fs,
		interpolator: interpolator,
	}
}

func (fp *FileProcessor) Process(absPathsFiles []string, vars vos.VariableSet) error {
	for _, absPathFile := range absPathsFiles {
		if _, exists := fp.backups[absPathFile]; !exists {
			originalContent, err := fp.fs.ReadFile(absPathFile)
			if err != nil {
				return fmt.Errorf("no se pudo leer el archivo de plantilla original %s: %w", absPathFile, err)
			}
			fp.backups[absPathFile] = originalContent
		}

		interpolatedContent, err := fp.interpolator.Interpolate(string(fp.backups[absPathFile]), vars)
		if err != nil {
			return fmt.Errorf("no se pudo interpolar la plantilla %s: %w", absPathFile, err)
		}

		if err := fp.fs.WriteFile(absPathFile, []byte(interpolatedContent)); err != nil {
			return fmt.Errorf("no se pudo escribir el archivo de plantilla interpolado %s: %w", absPathFile, err)
		}
	}
	return nil
}

func (fp *FileProcessor) Restore() error {
	var firstErr error
	for path, originalContent := range fp.backups {
		if err := fp.fs.WriteFile(path, originalContent); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("no se pudo restaurar el archivo de plantilla %s: %w", path, err)
			}
		}
	}
	return firstErr
}
