package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jairoprogramador/vex/internal/application"
)

// SourceCloner mantiene una caché de clones bajo root, uno por (url, ref). Usa el
// binario git para heredar las credenciales SSH/HTTPS del usuario.
//
// Un clon por ref evita que dos ejecuciones con refs distintos de un mismo
// repositorio se pisen el árbol de trabajo. Dos ejecuciones simultáneas con el
// mismo url y ref sí comparten clon; git lo protege con su propio lock.
type SourceCloner struct {
	root string
}

var _ application.SourceCloner = (*SourceCloner)(nil)

func NewSourceCloner(root string) *SourceCloner {
	return &SourceCloner{root: root}
}

func (c *SourceCloner) Ensure(ctx context.Context, repoURL, ref string) (application.Source, error) {
	if err := validate(repoURL, ref); err != nil {
		return application.Source{}, err
	}

	dir := filepath.Join(c.root, cacheKey(repoURL, ref))
	if err := c.ensureClone(ctx, repoURL, dir); err != nil {
		return application.Source{}, fmt.Errorf("clonar %s: %w", repoURL, err)
	}
	if _, err := runGit(ctx, dir, "fetch", "--quiet", "--prune", "--tags", "--force", "origin"); err != nil {
		return application.Source{}, fmt.Errorf("actualizar %s: %w", repoURL, err)
	}

	commit, err := resolveCommit(ctx, dir, ref)
	if err != nil {
		return application.Source{}, fmt.Errorf("resolver %q en %s: %w", ref, repoURL, err)
	}
	if err := checkout(ctx, dir, commit); err != nil {
		return application.Source{}, fmt.Errorf("posicionar %s en %s: %w", repoURL, commit, err)
	}
	return application.Source{Path: dir, Commit: commit}, nil
}

func validate(repoURL, ref string) error {
	switch {
	case repoURL == "":
		return errors.New("la url del repositorio está vacía")
	case ref == "":
		return fmt.Errorf("el ref de %s está vacío", repoURL)
	case strings.HasPrefix(repoURL, "-") || strings.HasPrefix(ref, "-"):
		// git lo leería como una opción.
		return fmt.Errorf("url o ref inválido: %q %q", repoURL, ref)
	}
	return nil
}

// cacheKey nombra el directorio del clon: estable, sin caracteres problemáticos
// del sistema de archivos y sin que dos (url, ref) distintos colisionen.
func cacheKey(repoURL, ref string) string {
	sum := sha256.Sum256([]byte(repoURL + "\x00" + ref))
	return hex.EncodeToString(sum[:8])
}

func isRepository(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// ensureClone clona en un directorio temporal y lo renombra, para que un clon
// interrumpido nunca quede como caché válida.
func (c *SourceCloner) ensureClone(ctx context.Context, repoURL, dir string) error {
	if isRepository(dir) {
		return nil
	}
	// Todo lo que hay bajo root es nuestro: un directorio sin .git es un resto roto.
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(c.root, 0o750); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(c.root, ".clone-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	if _, err := runGit(ctx, "", "clone", "--quiet", "--", repoURL, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, dir); err != nil && !isRepository(dir) {
		return err // si ya existe como repo, otro proceso ganó la carrera y sirve igual
	}
	return nil
}

// resolveCommit prueba, en orden, rama remota, tag y finalmente el ref tal cual
// (un SHA). La rama va primero porque tras el fetch es la más reciente.
func resolveCommit(ctx context.Context, dir, ref string) (string, error) {
	candidates := []string{"refs/remotes/origin/" + ref, "refs/tags/" + ref, ref}
	for _, candidate := range candidates {
		out, err := runGit(ctx, dir, "rev-parse", "--verify", "--quiet", candidate+"^{commit}")
		if err == nil {
			return strings.TrimSpace(out), nil
		}
	}
	return "", errors.New("no existe como rama, tag ni commit")
}

// checkout deja el árbol exactamente igual al commit: sin cambios locales ni
// archivos sobrantes de ejecuciones anteriores.
func checkout(ctx context.Context, dir, commit string) error {
	if _, err := runGit(ctx, dir, "checkout", "--quiet", "--detach", "--force", commit); err != nil {
		return err
	}
	_, err := runGit(ctx, dir, "clean", "--quiet", "-fdx")
	return err
}
