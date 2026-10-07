package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git no está disponible")
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}

// newOrigin crea un repositorio "remoto" con un commit en main.
func newOrigin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "--initial-branch=main")
	commitFile(t, dir, "README.md", "uno")
	return dir
}

func commitFile(t *testing.T, repo, name, content string) string {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(repo, name), []byte(content), 0o644))
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-qm", "cambio "+content)
	return git(t, repo, "rev-parse", "HEAD")
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

func TestSourceCloner_ClonaYResuelveLaRama(t *testing.T) {
	requireGit(t)
	origin := newOrigin(t)
	head := git(t, origin, "rev-parse", "HEAD")

	source, err := NewSourceCloner(t.TempDir()).Ensure(context.Background(), origin, "main")

	require.NoError(t, err)
	assert.Equal(t, head, source.Commit)
	assert.Equal(t, "uno", readFile(t, filepath.Join(source.Path, "README.md")))
}

func TestSourceCloner_ActualizaCuandoLaRamaAvanza(t *testing.T) {
	requireGit(t)
	origin := newOrigin(t)
	cloner := NewSourceCloner(t.TempDir())
	first, err := cloner.Ensure(context.Background(), origin, "main")
	require.NoError(t, err)

	newHead := commitFile(t, origin, "README.md", "dos")
	second, err := cloner.Ensure(context.Background(), origin, "main")

	require.NoError(t, err)
	assert.Equal(t, first.Path, second.Path, "reutiliza el mismo clon")
	assert.Equal(t, newHead, second.Commit)
	assert.NotEqual(t, first.Commit, second.Commit)
	assert.Equal(t, "dos", readFile(t, filepath.Join(second.Path, "README.md")))
}

func TestSourceCloner_ResuelveTagYCommit(t *testing.T) {
	requireGit(t)
	origin := newOrigin(t)
	tagged := git(t, origin, "rev-parse", "HEAD")
	git(t, origin, "tag", "v1")
	commitFile(t, origin, "README.md", "dos")
	cloner := NewSourceCloner(t.TempDir())

	byTag, err := cloner.Ensure(context.Background(), origin, "v1")
	require.NoError(t, err)
	bySHA, err := cloner.Ensure(context.Background(), origin, tagged)
	require.NoError(t, err)

	assert.Equal(t, tagged, byTag.Commit)
	assert.Equal(t, tagged, bySHA.Commit)
	assert.Equal(t, "uno", readFile(t, filepath.Join(byTag.Path, "README.md")))
}

func TestSourceCloner_UnTagQueSeMueveSeSigue(t *testing.T) {
	requireGit(t)
	origin := newOrigin(t)
	git(t, origin, "tag", "latest")
	cloner := NewSourceCloner(t.TempDir())
	_, err := cloner.Ensure(context.Background(), origin, "latest")
	require.NoError(t, err)

	moved := commitFile(t, origin, "README.md", "dos")
	git(t, origin, "tag", "-f", "latest")
	source, err := cloner.Ensure(context.Background(), origin, "latest")

	require.NoError(t, err)
	assert.Equal(t, moved, source.Commit)
}

func TestSourceCloner_RefsDistintosUsanClonesDistintos(t *testing.T) {
	requireGit(t)
	origin := newOrigin(t)
	git(t, origin, "branch", "feature")
	cloner := NewSourceCloner(t.TempDir())

	main, err := cloner.Ensure(context.Background(), origin, "main")
	require.NoError(t, err)
	feature, err := cloner.Ensure(context.Background(), origin, "feature")
	require.NoError(t, err)

	assert.NotEqual(t, main.Path, feature.Path)
}

func TestSourceCloner_DescartaCambiosLocalesDelClon(t *testing.T) {
	requireGit(t)
	origin := newOrigin(t)
	cloner := NewSourceCloner(t.TempDir())
	source, err := cloner.Ensure(context.Background(), origin, "main")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(source.Path, "README.md"), []byte("sucio"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(source.Path, "sobrante.txt"), []byte("x"), 0o644))

	again, err := cloner.Ensure(context.Background(), origin, "main")

	require.NoError(t, err)
	assert.Equal(t, "uno", readFile(t, filepath.Join(again.Path, "README.md")))
	assert.NoFileExists(t, filepath.Join(again.Path, "sobrante.txt"))
}

func TestSourceCloner_SeRecuperaDeUnDirectorioRoto(t *testing.T) {
	requireGit(t)
	origin := newOrigin(t)
	root := t.TempDir()
	broken := filepath.Join(root, cacheKey(origin, "main"))
	require.NoError(t, os.MkdirAll(broken, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(broken, "basura"), []byte("x"), 0o644))

	source, err := NewSourceCloner(root).Ensure(context.Background(), origin, "main")

	require.NoError(t, err)
	assert.Equal(t, broken, source.Path)
	assert.NoFileExists(t, filepath.Join(source.Path, "basura"))
}

func TestSourceCloner_NoDejaTemporalesTrasUnClonFallido(t *testing.T) {
	requireGit(t)
	root := t.TempDir()

	_, err := NewSourceCloner(root).Ensure(context.Background(), filepath.Join(t.TempDir(), "no-existe"), "main")

	require.Error(t, err)
	entries, readErr := os.ReadDir(root)
	require.NoError(t, readErr)
	assert.Empty(t, entries)
}

func TestSourceCloner_Errores(t *testing.T) {
	requireGit(t)
	origin := newOrigin(t)
	tests := []struct {
		name, url, ref, contains string
	}{
		{"url vacía", "", "main", "url"},
		{"ref vacío", origin, "", "ref"},
		{"url que parece opción", "--upload-pack=x", "main", "inválido"},
		{"ref que parece opción", origin, "--force", "inválido"},
		{"ref inexistente", origin, "no-existe", "no existe como rama, tag ni commit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewSourceCloner(t.TempDir()).Ensure(context.Background(), tt.url, tt.ref)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.contains)
		})
	}
}

func TestSourceCloner_RespetaLaCancelacion(t *testing.T) {
	requireGit(t)
	origin := newOrigin(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewSourceCloner(t.TempDir()).Ensure(ctx, origin, "main")

	assert.Error(t, err)
}
