// Package seen guarda en disco lo que el CLI recuerda de cada proyecto (ids vistos y catálogo del pipeline).
package seen

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jairoprogramador/vex/internal/application"
)

const fileFormat = 1

// FileStore guarda la memoria de cada proyecto en <root>/projects/<hash del id>/seen.json. Es una caché: se puede
// borrar sin perder nada que no se pueda volver a averiguar preguntando al motor.
type FileStore struct {
	root string
}

var _ application.RecentsStore = (*FileStore)(nil)

func NewFileStore(root string) *FileStore {
	return &FileStore{root: root}
}

type fileJSON struct {
	Format      int              `json:"format"`
	Attempts    []attemptJSON    `json:"attempts,omitempty"`
	Deployments []deploymentJSON `json:"deployments,omitempty"`
	Catalog     *catalogJSON     `json:"catalog,omitempty"`
}

type attemptJSON struct {
	ID          string    `json:"id"`
	Environment string    `json:"environment,omitempty"`
	UntilStep   string    `json:"until_step,omitempty"`
	Status      string    `json:"status,omitempty"`
	Cause       string    `json:"cause,omitempty"`
	At          time.Time `json:"at,omitzero"`
	Abandoned   bool      `json:"abandoned,omitempty"`
}

type deploymentJSON struct {
	ID          string    `json:"id"`
	Environment string    `json:"environment,omitempty"`
	At          time.Time `json:"at,omitzero"`
}

type catalogJSON struct {
	Environments []environmentJSON `json:"environments"`
	Steps        []stepJSON        `json:"steps"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

type environmentJSON struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Value       string `json:"value"`
	Protected   bool   `json:"protected,omitempty"`
}

type stepJSON struct {
	Name   string `json:"name"`
	Order  int    `json:"order"`
	Shared bool   `json:"shared,omitempty"`
}

// path usa un hash del id del proyecto: el id viene de vexconfig.yaml y no debe poder salirse del directorio.
func (s *FileStore) path(projectID string) string {
	sum := sha256.Sum256([]byte(projectID))
	return filepath.Join(s.root, "projects", hex.EncodeToString(sum[:8]), "seen.json")
}

// Load devuelve la memoria del proyecto; si no existe, o está ilegible, una vacía: perderla solo hace los
// comandos menos cómodos.
func (s *FileStore) Load(projectID string) (application.Recents, error) {
	data, err := os.ReadFile(s.path(projectID))
	if errors.Is(err, os.ErrNotExist) {
		return application.Recents{}, nil
	}
	if err != nil {
		return application.Recents{}, fmt.Errorf("leer la memoria del proyecto: %w", err)
	}
	var file fileJSON
	if err := json.Unmarshal(data, &file); err != nil || file.Format != fileFormat {
		return application.Recents{}, nil
	}
	return fromJSON(file), nil
}

// Save escribe de forma atómica: nunca queda un archivo a medias.
func (s *FileStore) Save(projectID string, recents application.Recents) error {
	path := s.path(projectID)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("crear la carpeta de la memoria del proyecto: %w", err)
	}
	data, err := json.Marshal(toJSON(recents))
	if err != nil {
		return fmt.Errorf("codificar la memoria del proyecto: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".seen-*")
	if err != nil {
		return fmt.Errorf("escribir la memoria del proyecto: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("escribir la memoria del proyecto: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("escribir la memoria del proyecto: %w", err)
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func toJSON(r application.Recents) fileJSON {
	file := fileJSON{Format: fileFormat}
	for _, a := range r.Attempts {
		file.Attempts = append(file.Attempts, attemptJSON{
			ID: a.ID, Environment: a.Environment, UntilStep: a.UntilStep,
			Status: string(a.Status), Cause: string(a.Cause), At: a.At, Abandoned: a.Abandoned,
		})
	}
	for _, d := range r.Deployments {
		file.Deployments = append(file.Deployments, deploymentJSON{ID: d.ID, Environment: d.Environment, At: d.At})
	}
	if r.Catalog != nil {
		catalog := &catalogJSON{UpdatedAt: r.Catalog.UpdatedAt}
		for _, e := range r.Catalog.Environments {
			catalog.Environments = append(catalog.Environments,
				environmentJSON{Name: e.Name, Description: e.Description, Value: e.Value, Protected: e.Protected})
		}
		for _, st := range r.Catalog.Steps {
			catalog.Steps = append(catalog.Steps, stepJSON{Name: st.Name, Order: st.Order, Shared: st.Shared})
		}
		file.Catalog = catalog
	}
	return file
}

func fromJSON(file fileJSON) application.Recents {
	var r application.Recents
	for _, a := range file.Attempts {
		r.Attempts = append(r.Attempts, application.RecentAttempt{
			ID: a.ID, Environment: a.Environment, UntilStep: a.UntilStep,
			Status: application.AttemptStatus(a.Status), Cause: application.AttemptCause(a.Cause), At: a.At, Abandoned: a.Abandoned,
		})
	}
	for _, d := range file.Deployments {
		r.Deployments = append(r.Deployments, application.RecentDeployment{ID: d.ID, Environment: d.Environment, At: d.At})
	}
	if file.Catalog != nil {
		catalog := &application.RecentCatalog{UpdatedAt: file.Catalog.UpdatedAt}
		for _, e := range file.Catalog.Environments {
			catalog.Environments = append(catalog.Environments,
				application.Environment{Name: e.Name, Description: e.Description, Value: e.Value, Protected: e.Protected})
		}
		for _, st := range file.Catalog.Steps {
			catalog.Steps = append(catalog.Steps, application.PipelineStep{Name: st.Name, Order: st.Order, Shared: st.Shared})
		}
		r.Catalog = catalog
	}
	return r
}
