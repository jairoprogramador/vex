package execution_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jairoprogramador/vex/internal/infrastructure/execution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupComplexTestDir crea una estructura de directorios compleja para las pruebas.
// Devuelve el directorio raíz y un mapa de las rutas de archivo que se crearon.
func setupComplexTestDir(t *testing.T) (string, map[string]string) {
	t.Helper()
	sourceDir, err := os.MkdirTemp("", "copy-source-*")
	require.NoError(t, err)

	filesToCreate := map[string]string{
		"terraform/main/main.tf":        "terraform main",
		"terraform/shared/variables.tf": "terraform shared vars",
		"angular/test/spec.ts":          "angular test spec",
		"angular/shared/service.ts":     "angular shared service",
		"root.txt":                      "root file",
	}

	for path, content := range filesToCreate {
		fullPath := filepath.Join(sourceDir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0644))
	}

	return sourceDir, filesToCreate
}

// assertFileExists verifica que un archivo exista en la ruta de destino y que su contenido coincida.
func assertFileExists(t *testing.T, destDir, relPath, expectedContent string) {
	t.Helper()
	destPath := filepath.Join(destDir, relPath)
	content, err := os.ReadFile(destPath)
	assert.NoError(t, err, "El archivo '%s' debería existir pero no se encontró", relPath)
	if err == nil {
		assert.Equal(t, expectedContent, string(content), "El contenido del archivo '%s' no coincide", relPath)
	}
}

// assertFileNotExists verifica que un archivo NO exista en la ruta de destino.
func assertFileNotExists(t *testing.T, destDir, relPath string) {
	t.Helper()
	destPath := filepath.Join(destDir, relPath)
	_, err := os.Stat(destPath)
	assert.True(t, os.IsNotExist(err), "El archivo '%s' no debería existir, pero fue encontrado", relPath)
}

func TestCopyWorkdir_CopySharedOnly(t *testing.T) {
	sourceDir, files := setupComplexTestDir(t)
	defer os.RemoveAll(sourceDir)

	destDir, err := os.MkdirTemp("", "copy-dest-*")
	require.NoError(t, err)
	defer os.RemoveAll(destDir)

	copier := execution.NewCopyWorkdir()
	err = copier.Copy(context.Background(), sourceDir, destDir, true)
	require.NoError(t, err)

	// ASSET: Deben existir los archivos dentro de 'shared'
	assertFileExists(t, destDir, "terraform/shared/variables.tf", files["terraform/shared/variables.tf"])
	assertFileExists(t, destDir, "angular/shared/service.ts", files["angular/shared/service.ts"])

	// ASSET: NO deben existir los archivos que no están en 'shared'
	assertFileNotExists(t, destDir, "terraform/main/main.tf")
	assertFileNotExists(t, destDir, "angular/test/spec.ts")
	assertFileNotExists(t, destDir, "root.txt")
}

func TestCopyWorkdir_CopyNonSharedOnly(t *testing.T) {
	sourceDir, files := setupComplexTestDir(t)
	defer os.RemoveAll(sourceDir)

	destDir, err := os.MkdirTemp("", "copy-dest-*")
	require.NoError(t, err)
	defer os.RemoveAll(destDir)

	copier := execution.NewCopyWorkdir()
	err = copier.Copy(context.Background(), sourceDir, destDir, false)
	require.NoError(t, err)

	// ASSET: Deben existir los archivos que NO están en 'shared'
	assertFileExists(t, destDir, "terraform/main/main.tf", files["terraform/main/main.tf"])
	assertFileExists(t, destDir, "angular/test/spec.ts", files["angular/test/spec.ts"])
	assertFileExists(t, destDir, "root.txt", files["root.txt"])

	// ASSET: NO deben existir los archivos que están en 'shared'
	assertFileNotExists(t, destDir, "terraform/shared/variables.tf")
	assertFileNotExists(t, destDir, "angular/shared/service.ts")
}

func TestCopyWorkdir_CopySharedOnly_NoSharedDirsExist(t *testing.T) {
	sourceDir, err := os.MkdirTemp("", "copy-source-no-shared-*")
	require.NoError(t, err)
	defer os.RemoveAll(sourceDir)

	// Crear solo archivos no compartidos
	path1 := filepath.Join(sourceDir, "terraform/main/main.tf")
	require.NoError(t, os.MkdirAll(filepath.Dir(path1), 0755))
	require.NoError(t, os.WriteFile(path1, []byte("content"), 0644))

	destDir, err := os.MkdirTemp("", "copy-dest-no-shared-*")
	require.NoError(t, err)
	defer os.RemoveAll(destDir)

	copier := execution.NewCopyWorkdir()
	err = copier.Copy(context.Background(), sourceDir, destDir, true) // isShared = true
	require.NoError(t, err)

	// ASSET: El directorio de destino debe estar vacío
	entries, err := os.ReadDir(destDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "El directorio de destino debería estar vacío ya que no había carpetas 'shared' para copiar")
}

func TestCopyWorkdir_SourceDoesNotExist(t *testing.T) {
	copier := execution.NewCopyWorkdir()
	// Llamar a Copy con una ruta fuente que no existe
	err := copier.Copy(context.Background(), "/path/to/nonexistent/source", "/tmp/dest", false)
	// ASSET: No se debe devolver ningún error
	assert.NoError(t, err)
}

func TestCopyWorkdir_SourceIsFile(t *testing.T) {
	// Crear un archivo temporal para usarlo como fuente
	sourceFile, err := os.CreateTemp("", "source-file-*")
	require.NoError(t, err)
	defer os.Remove(sourceFile.Name())

	copier := execution.NewCopyWorkdir()
	err = copier.Copy(context.Background(), sourceFile.Name(), "/tmp/dest", false)

	// ASSET: Se debe devolver un error
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no es un directorio")
}

func TestCopyWorkdir_ContextCancellation(t *testing.T) {
	sourceDir, _ := setupComplexTestDir(t)
	defer os.RemoveAll(sourceDir)

	destDir, err := os.MkdirTemp("", "copy-dest-cancel-*")
	require.NoError(t, err)
	defer os.RemoveAll(destDir)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// Esperar un poco para asegurarse de que el contexto se cancele
	time.Sleep(5 * time.Millisecond)

	copier := execution.NewCopyWorkdir()
	err = copier.Copy(ctx, sourceDir, destDir, false)

	// ASSET: Se debe devolver un error de contexto cancelado
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}
