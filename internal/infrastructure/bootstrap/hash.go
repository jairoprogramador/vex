// Package bootstrap lee la carpeta bootstrap/<nube>/ de un pipeline clonado: su manifiesto y la huella de su
// contenido. No conoce ninguna nube: el contrato del manifiesto lo interpreta el adaptador que lo tenga
// registrado.
package bootstrap

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// Límites de la carpeta de bootstrap. Forman parte de la especificación de la huella.
const (
	MaxFiles      = 64
	MaxTotalBytes = 1 << 20
)

// Códigos de HashError. Son los mismos en cualquier implementación de la huella.
const (
	CodeTooManyFiles  = "too_many_files"
	CodeTooLarge      = "too_large"
	CodeBadPath       = "bad_path"
	CodeDuplicatePath = "duplicate_path"
	CodeNotUTF8       = "not_utf8"
)

// File es un archivo de la carpeta de bootstrap.
type File struct {
	// Path es relativa a bootstrap/<nube>/ y usa "/" como separador.
	Path string
	// Content son los bytes originales: Hash los normaliza.
	Content []byte
}

// HashError dice por qué una carpeta no tiene huella. Path es la ruta culpable, si la hay.
type HashError struct {
	Code string
	Path string
}

func (e *HashError) Error() string {
	switch e.Code {
	case CodeTooManyFiles:
		return fmt.Sprintf("tiene más de %d archivos", MaxFiles)
	case CodeTooLarge:
		return fmt.Sprintf("ocupa más de %d bytes", MaxTotalBytes)
	case CodeBadPath:
		return fmt.Sprintf("la ruta %q no es válida", e.Path)
	case CodeDuplicatePath:
		return fmt.Sprintf("la ruta %q está repetida", e.Path)
	case CodeNotUTF8:
		return fmt.Sprintf("%q no es texto UTF-8", e.Path)
	}
	return e.Code
}

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Hash calcula la huella (versión 1) de una carpeta de bootstrap: SHA-256, en hexadecimal, de sus archivos en
// orden de ruta, cada uno como ruta, 0x00, longitud (uint64 big-endian) y contenido normalizado. La
// especificación y sus vectores se comparten con el backend (testdata/hash-vectors.json).
//
// Sin archivos no hay huella: devuelve "" sin error.
func Hash(files []File) (string, error) {
	if err := validate(files); err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", nil
	}

	sorted := slices.Clone(files)
	slices.SortFunc(sorted, func(a, b File) int { return strings.Compare(a.Path, b.Path) })

	digest := sha256.New()
	var length [8]byte
	for _, file := range sorted {
		content := normalize(file.Content)
		binary.BigEndian.PutUint64(length[:], uint64(len(content)))
		digest.Write([]byte(file.Path))
		digest.Write([]byte{0})
		digest.Write(length[:])
		digest.Write(content)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// validate aplica las comprobaciones en el orden de la especificación: el primer fallo decide el código.
func validate(files []File) error {
	if len(files) > MaxFiles {
		return &HashError{Code: CodeTooManyFiles}
	}
	total := 0
	for _, file := range files {
		total += len(file.Content)
	}
	if total > MaxTotalBytes {
		return &HashError{Code: CodeTooLarge}
	}
	for _, file := range files {
		if !validPath(file.Path) {
			return &HashError{Code: CodeBadPath, Path: file.Path}
		}
	}
	seen := make(map[string]bool, len(files))
	for _, file := range files {
		if seen[file.Path] {
			return &HashError{Code: CodeDuplicatePath, Path: file.Path}
		}
		seen[file.Path] = true
	}
	for _, file := range files {
		if !utf8.Valid(file.Content) {
			return &HashError{Code: CodeNotUTF8, Path: file.Path}
		}
	}
	return nil
}

func validPath(path string) bool {
	if path == "" || strings.ContainsAny(path, "\\\x00") {
		return false
	}
	for segment := range strings.SplitSeq(path, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false // cubre también la ruta absoluta: su primer segmento es vacío
		}
	}
	return true
}

// normalize quita un BOM inicial y convierte CR LF en LF, en una sola pasada: el mismo archivo guardado en
// Windows o en Unix da la misma huella. Un CR suelto y un BOM interior se conservan.
func normalize(content []byte) []byte {
	content = bytes.TrimPrefix(content, utf8BOM)
	return bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
}
