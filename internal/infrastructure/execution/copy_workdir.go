package execution

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/jairoprogramador/vex/internal/domain/execution/ports"
	"github.com/jairoprogramador/vex/internal/domain/execution/vos"
)

type CopyWorkdir struct{}

func NewCopyWorkdir() ports.CopyWorkdir {
	return &CopyWorkdir{}
}

func (c *CopyWorkdir) Copy(ctx context.Context, source, destination string, isShared bool) error {
	sourceInfo, err := os.Stat(source)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("no se pudo obtener información de la fuente '%s': %w", source, err)
	}
	if !sourceInfo.IsDir() {
		return fmt.Errorf("la fuente '%s' no es un directorio", source)
	}

	sourceMode := sourceInfo.Mode()

	return filepath.WalkDir(source, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// --- Lógica para isShared = true ---
		if isShared {
			if d.IsDir() {
				return nil
			}

			relPath, err := filepath.Rel(source, path)
			if err != nil {
				return fmt.Errorf("no se pudo calcular la ruta relativa para '%s': %w", path, err)
			}

			isInsideShared := false
			for _, part := range strings.Split(relPath, string(os.PathSeparator)) {
				if part == vos.SharedScope {
					isInsideShared = true
					break
				}
			}

			if !isInsideShared {
				return nil
			}

			destPath := filepath.Join(destination, relPath)

			if err := os.MkdirAll(filepath.Dir(destPath), sourceMode); err != nil {
				return fmt.Errorf("no se pudo crear el directorio padre '%s': %w", filepath.Dir(destPath), err)
			}

			fileInfo, err := d.Info()
			if err != nil {
				return fmt.Errorf("no se pudo obtener información de la entrada '%s': %w", path, err)
			}
			if err := copyFile(path, destPath, fileInfo.Mode()); err != nil {
				return fmt.Errorf("no se pudo copiar el archivo de '%s' a '%s': %w", path, destPath, err)
			}

			return nil
		}

		if d.IsDir() && d.Name() == vos.SharedScope {
			return filepath.SkipDir
		}

		relPath, err := filepath.Rel(source, path)
		if err != nil {
			return fmt.Errorf("no se pudo calcular la ruta relativa para '%s': %w", path, err)
		}

		if relPath == "." {
			return nil
		}

		destPath := filepath.Join(destination, relPath)

		fileInfo, err := d.Info()
		if err != nil {
			return fmt.Errorf("no se pudo obtener información de la entrada '%s': %w", path, err)
		}

		if d.IsDir() {
			if err := os.MkdirAll(destPath, fileInfo.Mode()); err != nil {
				return fmt.Errorf("no se pudo crear el directorio '%s': %w", destPath, err)
			}
		} else {
			if err := os.MkdirAll(filepath.Dir(destPath), sourceMode); err != nil {
				return fmt.Errorf("no se pudo crear el directorio padre '%s': %w", filepath.Dir(destPath), err)
			}
			if err := copyFile(path, destPath, fileInfo.Mode()); err != nil {
				return fmt.Errorf("no se pudo copiar el archivo de '%s' a '%s': %w", path, destPath, err)
			}
		}

		return nil
	})
}

// copyFile copia el contenido de un archivo a otro de manera eficiente,
// estableciendo los permisos correctos en el momento de la creación.
func copyFile(src, dst string, perm fs.FileMode) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	// Usamos OpenFile para crear el archivo con los permisos correctos desde el principio.
	destFile, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}
