package application

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Lo que el CLI recuerda de cada proyecto para que los comandos sean cómodos: los ids que ha visto (para decir
// `vex why` sin id, o `vex why 5ab306c` en vez de un UUID de 36 caracteres) y qué ambientes y pasos tiene el
// pipeline (para avisar de un error de tecleo y sugerir el nombre correcto sin preguntar al motor).

const (
	maxRecents = 100
	// minSuffixLength es lo mínimo que hay que teclear de un id: el sufijo de un UUID v7 es su parte aleatoria.
	minSuffixLength = 6
	// fullIDLength es la longitud de un UUID en texto.
	fullIDLength = 36
)

// RecentAttempt es un intento que el CLI ha ejecutado o ha visto en una lista.
type RecentAttempt struct {
	ID          string
	Environment string
	UntilStep   string
	// Status está vacío si no se sabe cómo terminó.
	Status AttemptStatus
	Cause  AttemptCause
	At     time.Time
	// Abandoned: alguien lo dio por perdido a mano; ya no está abierto aunque no tenga resultado.
	Abandoned bool
}

type RecentDeployment struct {
	ID          string
	Environment string
	At          time.Time
}

// RecentCatalog es lo último que se supo del pipeline del proyecto.
type RecentCatalog struct {
	Environments []Environment
	Steps        []PipelineStep
	UpdatedAt    time.Time
}

// Recents es la memoria de un proyecto.
type Recents struct {
	Attempts    []RecentAttempt
	Deployments []RecentDeployment
	Catalog     *RecentCatalog
}

// RecentsStore guarda la memoria de cada proyecto.
type RecentsStore interface {
	Load(projectID string) (Recents, error)
	Save(projectID string, recents Recents) error
}

var (
	// ErrUnknownID: ningún id visto termina así. Quien presenta sugiere cómo encontrarlo (`vex ls <ambiente>`).
	ErrUnknownID = errors.New("id desconocido")
	// ErrIDTooShort: con tan pocos caracteres podría ser cualquiera.
	ErrIDTooShort = errors.New("id demasiado corto")
)

// AmbiguousIDError: varios ids vistos terminan igual.
type AmbiguousIDError struct {
	Ref        string
	Candidates []string
}

func (e *AmbiguousIDError) Error() string {
	return fmt.Sprintf("el id %q es ambiguo: coincide con %d", e.Ref, len(e.Candidates))
}

// RecentsService es la memoria de un proyecto con las operaciones que los comandos necesitan. Es de mejor
// esfuerzo: perder la memoria nunca impide ejecutar nada, solo hace los comandos menos cómodos.
type RecentsService struct {
	store RecentsStore
	now   func() time.Time
}

func NewRecentsService(store RecentsStore) *RecentsService {
	return &RecentsService{store: store, now: time.Now}
}

// RememberAttempt guarda un intento nuevo o actualiza el que ya conocía con el mismo id.
func (s *RecentsService) RememberAttempt(projectID string, attempt RecentAttempt) error {
	return s.update(projectID, func(r *Recents) {
		for i := range r.Attempts {
			if r.Attempts[i].ID == attempt.ID {
				r.Attempts[i] = mergeAttempt(r.Attempts[i], attempt)
				return
			}
		}
		r.Attempts = append(r.Attempts, attempt)
		sort.Slice(r.Attempts, func(a, b int) bool { return r.Attempts[a].ID > r.Attempts[b].ID }) // UUID v7: más reciente primero
		if len(r.Attempts) > maxRecents {
			r.Attempts = r.Attempts[:maxRecents]
		}
	})
}

// mergeAttempt completa lo conocido con lo nuevo sin borrar datos con vacíos.
func mergeAttempt(old, update RecentAttempt) RecentAttempt {
	if update.Environment != "" {
		old.Environment = update.Environment
	}
	if update.UntilStep != "" {
		old.UntilStep = update.UntilStep
	}
	if update.Status != "" {
		old.Status = update.Status
	}
	if update.Cause != "" {
		old.Cause = update.Cause
	}
	if !update.At.IsZero() {
		old.At = update.At
	}
	old.Abandoned = old.Abandoned || update.Abandoned
	return old
}

// RememberAttempts guarda varios de una vez (una lista de `vex ls`).
func (s *RecentsService) RememberAttempts(projectID string, summaries []AttemptSummary) error {
	for _, a := range summaries {
		err := s.RememberAttempt(projectID, RecentAttempt{
			ID: a.ID, Environment: a.Environment, UntilStep: a.UntilStep, Status: a.Status, Cause: a.Cause, At: a.StartedAt,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *RecentsService) RememberDeployments(projectID string, deployments []Deployment) error {
	return s.update(projectID, func(r *Recents) {
		known := map[string]bool{}
		for _, d := range r.Deployments {
			known[d.ID] = true
		}
		for _, d := range deployments {
			if !known[d.ID] {
				r.Deployments = append(r.Deployments, RecentDeployment{ID: d.ID, Environment: d.Environment, At: d.At})
			}
		}
		sort.Slice(r.Deployments, func(a, b int) bool { return r.Deployments[a].ID > r.Deployments[b].ID })
		if len(r.Deployments) > maxRecents {
			r.Deployments = r.Deployments[:maxRecents]
		}
	})
}

// RememberCatalog guarda los ambientes y pasos que el motor acaba de decir.
func (s *RecentsService) RememberCatalog(projectID string, environments []Environment, steps []PipelineStep) error {
	return s.update(projectID, func(r *Recents) {
		r.Catalog = &RecentCatalog{Environments: environments, Steps: steps, UpdatedAt: s.now()}
	})
}

// RememberEnvironments guarda los ambientes del pipeline sin tocar los pasos que ya se conocían.
func (s *RecentsService) RememberEnvironments(projectID string, environments []Environment) error {
	return s.update(projectID, func(r *Recents) {
		catalog := RecentCatalog{}
		if r.Catalog != nil {
			catalog = *r.Catalog
		}
		catalog.Environments, catalog.UpdatedAt = environments, s.now()
		r.Catalog = &catalog
	})
}

// RememberSteps guarda los pasos del pipeline sin tocar los ambientes que ya se conocían.
func (s *RecentsService) RememberSteps(projectID string, steps []PipelineStep) error {
	return s.update(projectID, func(r *Recents) {
		catalog := RecentCatalog{}
		if r.Catalog != nil {
			catalog = *r.Catalog
		}
		catalog.Steps, catalog.UpdatedAt = steps, s.now()
		r.Catalog = &catalog
	})
}

func (s *RecentsService) update(projectID string, change func(*Recents)) error {
	recents, err := s.store.Load(projectID)
	if err != nil {
		return err
	}
	change(&recents)
	return s.store.Save(projectID, recents)
}

// LastAttempt es el intento más reciente que se recuerda; con onlyFailed, el más reciente que falló.
func (s *RecentsService) LastAttempt(projectID string, onlyFailed bool) (RecentAttempt, bool, error) {
	recents, err := s.store.Load(projectID)
	if err != nil {
		return RecentAttempt{}, false, err
	}
	for _, a := range recents.Attempts {
		if !onlyFailed || a.Status == AttemptFailed {
			return a, true, nil
		}
	}
	return RecentAttempt{}, false, nil
}

// FindAttempt devuelve lo que se recuerda de un intento por su id completo.
func (s *RecentsService) FindAttempt(projectID, id string) (RecentAttempt, bool, error) {
	recents, err := s.store.Load(projectID)
	if err != nil {
		return RecentAttempt{}, false, err
	}
	for _, a := range recents.Attempts {
		if a.ID == id {
			return a, true, nil
		}
	}
	return RecentAttempt{}, false, nil
}

// OpenAttempts son los intentos recordados que no tienen resultado ni fueron abandonados: los que podrían estar
// ocupando un ambiente. El más reciente va primero.
func (s *RecentsService) OpenAttempts(projectID string) ([]RecentAttempt, error) {
	recents, err := s.store.Load(projectID)
	if err != nil {
		return nil, err
	}
	var open []RecentAttempt
	for _, a := range recents.Attempts {
		if a.Status == "" && !a.Abandoned {
			open = append(open, a)
		}
	}
	return open, nil
}

// FindDeployment devuelve lo que se recuerda de un despliegue por su id completo.
func (s *RecentsService) FindDeployment(projectID, id string) (RecentDeployment, bool, error) {
	recents, err := s.store.Load(projectID)
	if err != nil {
		return RecentDeployment{}, false, err
	}
	for _, d := range recents.Deployments {
		if d.ID == id {
			return d, true, nil
		}
	}
	return RecentDeployment{}, false, nil
}

// AbandonedIDs son los intentos que este CLI abandonó. El motor no lo dice en las listas de intentos.
func (s *RecentsService) AbandonedIDs(projectID string) (map[string]bool, error) {
	recents, err := s.store.Load(projectID)
	if err != nil {
		return nil, err
	}
	abandoned := map[string]bool{}
	for _, a := range recents.Attempts {
		if a.Abandoned {
			abandoned[a.ID] = true
		}
	}
	return abandoned, nil
}

// LastDeployment es el despliegue más reciente que se recuerda.
func (s *RecentsService) LastDeployment(projectID string) (RecentDeployment, bool, error) {
	recents, err := s.store.Load(projectID)
	if err != nil || len(recents.Deployments) == 0 {
		return RecentDeployment{}, false, err
	}
	return recents.Deployments[0], true, nil
}

// ResolveAttempt devuelve el id completo de un intento a partir de lo que tecleó la persona: el id entero o el
// final (mínimo 6 caracteres).
func (s *RecentsService) ResolveAttempt(projectID, ref string) (string, error) {
	recents, err := s.store.Load(projectID)
	if err != nil {
		return "", err
	}
	ids := make([]string, len(recents.Attempts))
	for i, a := range recents.Attempts {
		ids[i] = a.ID
	}
	return resolveID(ids, ref)
}

// ResolveDeployment es ResolveAttempt para despliegues.
func (s *RecentsService) ResolveDeployment(projectID, ref string) (string, error) {
	recents, err := s.store.Load(projectID)
	if err != nil {
		return "", err
	}
	ids := make([]string, len(recents.Deployments))
	for i, d := range recents.Deployments {
		ids[i] = d.ID
	}
	return resolveID(ids, ref)
}

// Catalog es lo último que se supo del pipeline, si se supo algo.
func (s *RecentsService) Catalog(projectID string) (RecentCatalog, bool, error) {
	recents, err := s.store.Load(projectID)
	if err != nil || recents.Catalog == nil {
		return RecentCatalog{}, false, err
	}
	return *recents.Catalog, true, nil
}

// EnvironmentOf dice a qué ambiente pertenece un intento o un despliegue recordado.
func (s *RecentsService) EnvironmentOf(projectID, id string) (string, bool, error) {
	recents, err := s.store.Load(projectID)
	if err != nil {
		return "", false, err
	}
	for _, a := range recents.Attempts {
		if a.ID == id {
			return a.Environment, true, nil
		}
	}
	for _, d := range recents.Deployments {
		if d.ID == id {
			return d.Environment, true, nil
		}
	}
	return "", false, nil
}

func resolveID(ids []string, ref string) (string, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	if len(ref) >= fullIDLength {
		return ref, nil // un id completo siempre vale: puede ser de un intento que este CLI no ha visto
	}
	if len(ref) < minSuffixLength {
		return "", ErrIDTooShort
	}
	var matches []string
	for _, id := range ids {
		if strings.HasSuffix(strings.ToLower(id), ref) {
			matches = append(matches, id)
		}
	}
	switch len(matches) {
	case 0:
		return "", ErrUnknownID
	case 1:
		return matches[0], nil
	}
	return "", &AmbiguousIDError{Ref: ref, Candidates: matches}
}

// ClosestName devuelve, de entre options, el nombre más parecido a typed si es lo bastante parecido como para
// ser un error de tecleo (distancia de edición pequeña respecto a la longitud, o uno contiene al otro).
func ClosestName(typed string, options []string) (string, bool) {
	typed = strings.ToLower(strings.TrimSpace(typed))
	if typed == "" {
		return "", false
	}
	best, bestDistance := "", 1<<30
	for _, option := range options {
		candidate := strings.ToLower(option)
		distance := editDistance(typed, candidate)
		if strings.Contains(candidate, typed) || strings.Contains(typed, candidate) {
			distance = min(distance, 1)
		}
		if distance < bestDistance {
			best, bestDistance = option, distance
		}
	}
	limit := max(1, len(typed)/3)
	if best == "" || bestDistance > limit {
		return "", false
	}
	return best, true
}

// editDistance es la distancia entre dos cadenas contando como una sola edición insertar, borrar, cambiar una
// letra o intercambiar dos contiguas (`pord` por `prod`, el error de tecleo más común).
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	table := make([][]int, len(ra)+1)
	for i := range table {
		table[i] = make([]int, len(rb)+1)
		table[i][0] = i
	}
	for j := range table[0] {
		table[0][j] = j
	}
	for i := 1; i <= len(ra); i++ {
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			table[i][j] = min(table[i-1][j]+1, table[i][j-1]+1, table[i-1][j-1]+cost)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				table[i][j] = min(table[i][j], table[i-2][j-2]+1)
			}
		}
	}
	return table[len(ra)][len(rb)]
}

// CheckEnvironment avisa de un ambiente mal tecleado si se sabe cuáles hay. Sin catálogo recordado no opina: no
// se puede distinguir un ambiente nuevo de un error de tecleo.
func (s *RecentsService) CheckEnvironment(projectID, environment string) error {
	catalog, ok, err := s.Catalog(projectID)
	if err != nil || !ok || len(catalog.Environments) == 0 {
		return nil
	}
	known := make([]string, len(catalog.Environments))
	for i, e := range catalog.Environments {
		if e.Value == environment {
			return nil
		}
		known[i] = e.Value
	}
	suggestion, _ := ClosestName(environment, known)
	return &UnknownEnvironmentError{Name: environment, Known: known, Suggestion: suggestion}
}
