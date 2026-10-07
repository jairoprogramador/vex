package seen

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jairoprogramador/vex/internal/application"
)

var at0 = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

func sample() application.Recents {
	return application.Recents{
		Attempts: []application.RecentAttempt{
			{ID: "i2", Environment: "sand", UntilStep: "test", Status: application.AttemptFailed, Cause: application.CauseError, At: at0},
			{ID: "i1", Environment: "prod", Abandoned: true},
		},
		Deployments: []application.RecentDeployment{{ID: "d1", Environment: "prod", At: at0}},
		Catalog: &application.RecentCatalog{
			Environments: []application.Environment{{Name: "production", Description: "d", Value: "prod", Protected: true}},
			Steps:        []application.PipelineStep{{Name: "test", Order: 1, Shared: true}},
			UpdatedAt:    at0,
		},
	}
}

func TestFileStore_GuardaYLeeTodo(t *testing.T) {
	store := NewFileStore(t.TempDir())

	require.NoError(t, store.Save("proyecto-1", sample()))
	got, err := store.Load("proyecto-1")

	require.NoError(t, err)
	assert.Equal(t, sample(), got)
}

func TestFileStore_UnProyectoSinMemoriaDevuelveVacio(t *testing.T) {
	got, err := NewFileStore(t.TempDir()).Load("nuevo")

	require.NoError(t, err)
	assert.Equal(t, application.Recents{}, got)
}

func TestFileStore_CadaProyectoTieneLaSuya(t *testing.T) {
	store := NewFileStore(t.TempDir())
	require.NoError(t, store.Save("a", sample()))

	other, err := store.Load("b")

	require.NoError(t, err)
	assert.Empty(t, other.Attempts)
}

func TestFileStore_UnArchivoCorruptoSeLeeComoVacio(t *testing.T) {
	root := t.TempDir()
	store := NewFileStore(root)
	require.NoError(t, store.Save("p", sample()))
	require.NoError(t, os.WriteFile(store.path("p"), []byte("{esto no es json"), 0o600))

	got, err := store.Load("p")

	require.NoError(t, err, "perder la memoria no puede impedir ejecutar un comando")
	assert.Empty(t, got.Attempts)
	require.NoError(t, store.Save("p", sample()), "y se puede volver a escribir encima")
}

func TestFileStore_UnFormatoDesconocidoSeIgnora(t *testing.T) {
	store := NewFileStore(t.TempDir())
	require.NoError(t, store.Save("p", sample()))
	require.NoError(t, os.WriteFile(store.path("p"), []byte(`{"format":99,"attempts":[{"id":"x"}]}`), 0o600))

	got, err := store.Load("p")

	require.NoError(t, err)
	assert.Empty(t, got.Attempts)
}

func TestFileStore_UnIdDeProyectoRaroNoSaleDeSuCarpeta(t *testing.T) {
	root := t.TempDir()
	store := NewFileStore(root)

	require.NoError(t, store.Save("../../../etc/passwd", sample()))

	assert.True(t, filepath.HasPrefix(store.path("../../../etc/passwd"), root))
	_, err := os.Stat(filepath.Join(root, "..", "..", "..", "etc", "passwd", "seen.json"))
	assert.Error(t, err)
}

func TestFileStore_EscribeConPermisosPrivadosYSinDejarTemporales(t *testing.T) {
	root := t.TempDir()
	store := NewFileStore(root)

	require.NoError(t, store.Save("p", sample()))
	require.NoError(t, store.Save("p", sample()))

	info, err := os.Stat(store.path("p"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	entries, err := os.ReadDir(filepath.Dir(store.path("p")))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "solo seen.json: sin archivos temporales")
}
