package state

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jairoprogramador/vex/internal/domain/state/ports"
	"github.com/jairoprogramador/vex/internal/domain/state/vos"
	gitignore "github.com/sabhiram/go-gitignore"
)

// Sha256FingerprintService implementa FingerprintService usando el algoritmo SHA-256.
type Sha256FingerprintService struct{}

// NewSha256FingerprintService crea una nueva instancia de Sha256FingerprintService.
func NewSha256FingerprintService() ports.FingerprintService {
	return &Sha256FingerprintService{}
}

// FromFile calcula el fingerprint de un único archivo.
func (s *Sha256FingerprintService) FromFile(filePath string) (vos.Fingerprint, error) {
	file, err := os.Open(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return vos.Fingerprint{}, nil
		}
		return vos.Fingerprint{}, fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return vos.Fingerprint{}, fmt.Errorf("failed to read file %s for hashing: %w", filePath, err)
	}

	hashBytes := hasher.Sum(nil)
	return vos.NewFingerprint(hex.EncodeToString(hashBytes))
}

// FromDirectory calcula el fingerprint de un directorio, respetando .gitignore.
func (s *Sha256FingerprintService) FromDirectory(dirPath string) (vos.Fingerprint, error) {
	var ignorer *gitignore.GitIgnore
	gitignorePath := filepath.Join(dirPath, ".gitignore")
	if _, err := os.Stat(gitignorePath); err == nil {
		ignorer, err = gitignore.CompileIgnoreFile(gitignorePath)
		if err != nil {
			return vos.Fingerprint{}, fmt.Errorf("failed to compile .gitignore file: %w", err)
		}
	}

	// fileHashes almacenará la ruta relativa y el hash de cada archivo.
	var fileHashes []string

	err := filepath.WalkDir(dirPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Obtener la ruta relativa para las comprobaciones y el hash final.
		relPath, err := filepath.Rel(dirPath, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path for %s: %w", path, err)
		}

		// No incluir el propio directorio raíz en las comprobaciones de ignore.
		if relPath == "." {
			return nil
		}

		// Excluir el directorio .git.
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}

		// Comprobar si la ruta debe ser ignorada por .gitignore.
		if ignorer != nil && ignorer.MatchesPath(relPath) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Si es un directorio, no hacemos nada más.
		if d.IsDir() {
			return nil
		}

		// Calcular el hash del archivo.
		fileFingerprint, err := s.FromFile(path)
		if err != nil {
			return fmt.Errorf("failed to get fingerprint for file %s: %w", path, err)
		}

		// Añadir la ruta relativa y el hash a nuestra lista para el hash final.
		// Se usa un separador para asegurar que no haya colisiones con nombres de archivo.
		fileHashes = append(fileHashes, fmt.Sprintf("%s:%s", relPath, fileFingerprint.String()))

		return nil
	})

	if err != nil {
		return vos.Fingerprint{}, fmt.Errorf("failed to walk directory %s: %w", dirPath, err)
	}

	// ¡Paso crítico! Ordenar los hashes para una firma final estable.
	sort.Strings(fileHashes)

	// Unir todas las firmas en un solo string y calcular el hash final.
	finalHasher := sha256.New()
	finalHasher.Write([]byte(strings.Join(fileHashes, "\n")))

	hashBytes := finalHasher.Sum(nil)
	return vos.NewFingerprint(hex.EncodeToString(hashBytes))
}
