package versioning

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"sort"

	"github.com/Masterminds/semver/v3"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/jairoprogramador/vex/internal/domain/versioning/ports"
	"github.com/jairoprogramador/vex/internal/domain/versioning/vos"
)

// semverTagRegex es una expresión regular para validar y extraer versiones de tags.
var semverTagRegex = regexp.MustCompile(`^v?(\d+\.\d+\.\d+)$`)

// GoGitRepository es una implementación de GitRepository usando la librería go-git.
type GoGitRepository struct{}

// NewGoGitRepository crea una nueva instancia de GoGitRepository.
func NewGoGitRepository() ports.GitRepository {
	return &GoGitRepository{}
}

// GetLastCommit obtiene el último commit de la rama actual (HEAD).
func (r *GoGitRepository) GetLastCommit(ctx context.Context, repoPath string) (*vos.Commit, error) {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, fmt.Errorf("error al abrir el repositorio: %w", err)
	}

	headRef, err := repo.Head()
	if err != nil {
		return nil, fmt.Errorf("error al obtener HEAD: %w", err)
	}

	lastCommit, err := repo.CommitObject(headRef.Hash())
	if err != nil {
		return nil, fmt.Errorf("error al obtener el commit object: %w", err)
	}

	return &vos.Commit{
		Hash:    lastCommit.Hash.String(),
		Message: lastCommit.Message,
		Author:  lastCommit.Author.Name,
		Date:    lastCommit.Author.When,
	}, nil
}

// GetCommitsSinceTag obtiene todos los commits desde un tag específico.
// Si lastTag está vacío, devuelve todos los commits.
func (r *GoGitRepository) GetCommitsSinceTag(ctx context.Context, repoPath string, lastTag string) ([]*vos.Commit, error) {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, fmt.Errorf("error al abrir el repositorio: %w", err)
	}

	headRef, err := repo.Head()
	if err != nil {
		return nil, fmt.Errorf("error al obtener HEAD: %w", err)
	}

	startHash := headRef.Hash() // Por defecto, empezamos desde HEAD
	var stopHash plumbing.Hash  // Hash donde nos detendremos (si hay tag)

	if lastTag != "" {
		tagRef, err := repo.Tag(lastTag)
		if err != nil {
			ref, errRef := repo.Reference(plumbing.ReferenceName("refs/tags/"+lastTag), true)
			if errRef != nil {
				return nil, fmt.Errorf("no se pudo encontrar el tag '%s': %w", lastTag, errRef)
			}
			stopHash = ref.Hash()
		} else {
			stopHash = tagRef.Hash()
		}
	}

	commitIter, err := repo.Log(&git.LogOptions{From: startHash})
	if err != nil {
		return nil, fmt.Errorf("error al obtener el log de commits: %w", err)
	}

	const commitLimit = 250
	var commits []*vos.Commit
	count := 0

	err = commitIter.ForEach(func(c *object.Commit) error {
		// Condición de parada: límite de commits si no hay tag
		if lastTag == "" && count >= commitLimit {
			return io.EOF
		}

		// Condición de parada: encontramos el commit del tag anterior
		if lastTag != "" && c.Hash == stopHash {
			return io.EOF
		}

		commits = append(commits, &vos.Commit{
			Hash:    c.Hash.String(),
			Message: c.Message,
			Author:  c.Author.Name,
			Date:    c.Author.When,
		})
		count++
		return nil
	})

	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("error al iterar los commits: %w", err)
	}

	return commits, nil
}

// GetLastSemverTag obtiene el último tag semántico del repositorio.
func (r *GoGitRepository) GetLastSemverTag(ctx context.Context, repoPath string) (string, error) {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return "", fmt.Errorf("error al abrir el repositorio: %w", err)
	}

	tagIter, err := repo.Tags()
	if err != nil {
		return "", fmt.Errorf("error al obtener los tags: %w", err)
	}

	var versions semver.Collection
	err = tagIter.ForEach(func(ref *plumbing.Reference) error {
		tagName := ref.Name().Short()
		if semverTagRegex.MatchString(tagName) {
			v, err := semver.NewVersion(tagName)
			if err == nil {
				versions = append(versions, v)
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("error al iterar los tags: %w", err)
	}

	if len(versions) == 0 {
		return "", fmt.Errorf("no se encontraron tags con formato semántico")
	}

	// Ordenar las versiones de más antigua a más nueva.
	sort.Sort(versions)

	// El último elemento de la colección ordenada es el más reciente.
	lastVersion := versions[len(versions)-1]
	return lastVersion.Original(), nil
}

// CreateTagForCommit crea un nuevo tag apuntando a un commit específico.
// Devuelve un error si el tag ya existe o el commit no se encuentra.
func (r *GoGitRepository) CreateTagForCommit(ctx context.Context, repoPath string, commitHash string, tagName string) error {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("error al abrir el repositorio: %w", err)
	}

	// 1. Validar que el commit existe.
	hash := plumbing.NewHash(commitHash)
	if _, err := repo.CommitObject(hash); err != nil {
		return fmt.Errorf("no se pudo encontrar el commit con hash '%s': %w", commitHash, err)
	}

	// 2. Verificar que el tag no exista previamente.
	tagRefName := plumbing.ReferenceName("refs/tags/" + tagName)
	_, err = repo.Reference(tagRefName, true)
	if err == nil {
		// No hay error, significa que el tag ya existe.
		return fmt.Errorf("el tag '%s' ya existe en el repositorio", tagName)
	}
	// Si el error es 'plumbing.ErrReferenceNotFound', es el escenario esperado.
	// Cualquier otro error es un problema real.
	if err != plumbing.ErrReferenceNotFound {
		return fmt.Errorf("error al verificar la existencia del tag '%s': %w", tagName, err)
	}

	// 3. Crear el tag (referencia lightweight).
	ref := plumbing.NewHashReference(tagRefName, hash)

	if err := repo.Storer.SetReference(ref); err != nil {
		return fmt.Errorf("no se pudo crear el tag '%s': %w", tagName, err)
	}

	return nil
}
