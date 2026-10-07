package application

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

// Consultas al historial del motor: intentos, despliegues y lanzamientos. Ninguna ejecuta ni escribe nada en el
// pipeline, y ninguna necesita clonar: solo el historial montado.

// ErrNoRecentAttempt: se pidió «el último intento» y este proyecto todavía no tiene ninguno recordado.
var ErrNoRecentAttempt = errors.New("no hay ningún intento reciente en este proyecto")

// UnknownEnvironmentError: el ambiente que se tecleó no existe en el pipeline, según lo último que se supo de él.
type UnknownEnvironmentError struct {
	Name string
	// Known son los ambientes del pipeline; Suggestion, el más parecido al que se tecleó (puede estar vacío).
	Known      []string
	Suggestion string
}

func (e *UnknownEnvironmentError) Error() string {
	return fmt.Sprintf("el ambiente %q no existe en el pipeline", e.Name)
}

// queryEngine es lo que las consultas necesitan del motor.
type queryEngine interface {
	Attempts(ctx context.Context, spec ContainerSpec, environment string) ([]AttemptSummary, error)
	AttemptDetail(ctx context.Context, spec ContainerSpec, attemptID string) (AttemptDetail, error)
	Logs(ctx context.Context, spec ContainerSpec, req LogsRequest) ([]CommandOutput, error)
	Deployments(ctx context.Context, spec ContainerSpec, environment string) ([]Deployment, error)
	Releases(ctx context.Context, spec ContainerSpec, environment string) ([]Release, error)
}

// QueryService responde las consultas de `vex ls`, `show`, `log`, `deployments` y `releases`.
type QueryService struct {
	workspace *Workspace
	engine    queryEngine
	recents   *RecentsService
}

func NewQueryService(workspace *Workspace, engine queryEngine, recents *RecentsService) *QueryService {
	return &QueryService{workspace: workspace, engine: engine, recents: recents}
}

// session es lo que toda consulta necesita antes de preguntar: el proyecto y un contenedor con el historial.
type session struct {
	projectID string
	spec      ContainerSpec
}

func (s *QueryService) open(ctx context.Context) (session, error) {
	history, err := s.workspace.OpenHistory(ctx)
	if err != nil {
		return session{}, err
	}
	return session{projectID: history.ProjectID, spec: history.Spec}, nil
}

// --- Intentos ---

// AttemptList son los últimos intentos de un ambiente, del más reciente al más antiguo.
type AttemptList struct {
	Environment string
	Attempts    []AttemptSummary
	// Total es cuántos intentos tiene el ambiente en total; Attempts puede traer solo los últimos.
	Total int
}

// ListAttempts devuelve los últimos limit intentos del ambiente (todos si limit <= 0).
func (s *QueryService) ListAttempts(ctx context.Context, environment string, limit int) (AttemptList, error) {
	sess, err := s.open(ctx)
	if err != nil {
		return AttemptList{}, err
	}
	if err := s.recents.CheckEnvironment(sess.projectID, environment); err != nil {
		return AttemptList{}, err
	}
	attempts, err := s.engine.Attempts(ctx, sess.spec, environment)
	if err != nil {
		return AttemptList{}, err
	}
	// Recordar los ids vistos permite luego `vex why 5ab306c` sobre intentos de otras personas.
	_ = s.recents.RememberAttempts(sess.projectID, attempts)
	markAbandoned(attempts, s.recents, sess.projectID)

	list := AttemptList{Environment: environment, Total: len(attempts), Attempts: newestFirst(attempts)}
	if limit > 0 && len(list.Attempts) > limit {
		list.Attempts = list.Attempts[:limit]
	}
	return list, nil
}

// markAbandoned completa en la lista los intentos que este CLI abandonó: el motor solo lo dice en el detalle.
func markAbandoned(attempts []AttemptSummary, recents *RecentsService, projectID string) {
	abandoned, err := recents.AbandonedIDs(projectID)
	if err != nil || len(abandoned) == 0 {
		return
	}
	for i := range attempts {
		if attempts[i].Status == "" && abandoned[attempts[i].ID] {
			attempts[i].Abandoned = true
		}
	}
}

func newestFirst[T any](oldestFirst []T) []T {
	reversed := make([]T, len(oldestFirst))
	for i, item := range oldestFirst {
		reversed[len(oldestFirst)-1-i] = item
	}
	return reversed
}

// ShowAttempt devuelve el detalle de un intento. ref es el id completo o su final; vacío es el último intento
// que se recuerda en este proyecto.
func (s *QueryService) ShowAttempt(ctx context.Context, ref string) (AttemptDetail, error) {
	sess, err := s.open(ctx)
	if err != nil {
		return AttemptDetail{}, err
	}
	id, err := s.resolveAttempt(sess.projectID, ref, false)
	if err != nil {
		return AttemptDetail{}, err
	}
	detail, err := s.engine.AttemptDetail(ctx, sess.spec, id)
	if err != nil {
		return AttemptDetail{}, err
	}
	_ = s.recents.RememberAttempt(sess.projectID, RecentAttempt{
		ID: detail.ID, Environment: detail.Environment, UntilStep: detail.UntilStep,
		Status: detail.Status, Cause: detail.Cause, At: detail.StartedAt,
	})
	return detail, nil
}

// resolveAttempt convierte lo que tecleó la persona en un id completo. Sin nada, usa el último intento que se
// recuerda (el último fallido si onlyFailed).
func (s *QueryService) resolveAttempt(projectID, ref string, onlyFailed bool) (string, error) {
	if ref != "" {
		return s.recents.ResolveAttempt(projectID, ref)
	}
	last, ok, err := s.recents.LastAttempt(projectID, onlyFailed)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrNoRecentAttempt
	}
	return last.ID, nil
}

// AttemptLogs es la salida de los comandos de un intento.
type AttemptLogs struct {
	AttemptID string
	Outputs   []CommandOutput
}

// Logs devuelve la salida de los comandos de un intento (solo los fallidos con onlyFailed). Sin ref usa el último
// intento recordado; si no hay ninguno, deja que el motor elija el último que abrió en cualquier ambiente.
func (s *QueryService) Logs(ctx context.Context, ref string, onlyFailed bool) (AttemptLogs, error) {
	sess, err := s.open(ctx)
	if err != nil {
		return AttemptLogs{}, err
	}
	id, err := s.resolveAttempt(sess.projectID, ref, false)
	if errors.Is(err, ErrNoRecentAttempt) {
		id, err = "", nil
	}
	if err != nil {
		return AttemptLogs{}, err
	}
	outputs, err := s.engine.Logs(ctx, sess.spec, LogsRequest{AttemptID: id, OnlyFailed: onlyFailed})
	if err != nil {
		return AttemptLogs{}, err
	}
	return AttemptLogs{AttemptID: id, Outputs: outputs}, nil
}

// --- Despliegues y lanzamientos ---

// DeploymentView es un despliegue con el lanzamiento que lo hizo visible, si lo hubo.
type DeploymentView struct {
	Deployment Deployment
	// Release es el último lanzamiento de este despliegue; nil si nunca se lanzó.
	Release *Release
	// Current dice si es el que está lanzado ahora en el ambiente.
	Current bool
}

// DeploymentList son los despliegues de un ambiente, del más reciente al más antiguo.
type DeploymentList struct {
	Environment string
	Items       []DeploymentView
	Total       int
}

// ListDeployments devuelve los últimos limit despliegues del ambiente (todos si limit <= 0), marcando cuál está
// lanzado.
func (s *QueryService) ListDeployments(ctx context.Context, environment string, limit int) (DeploymentList, error) {
	sess, err := s.open(ctx)
	if err != nil {
		return DeploymentList{}, err
	}
	if err := s.recents.CheckEnvironment(sess.projectID, environment); err != nil {
		return DeploymentList{}, err
	}
	deployments, err := s.engine.Deployments(ctx, sess.spec, environment)
	if err != nil {
		return DeploymentList{}, err
	}
	releases, err := s.engine.Releases(ctx, sess.spec, environment)
	if err != nil {
		return DeploymentList{}, err
	}
	_ = s.recents.RememberDeployments(sess.projectID, deployments)

	items := deploymentViews(deployments, releases)
	list := DeploymentList{Environment: environment, Total: len(items), Items: items}
	if limit > 0 && len(list.Items) > limit {
		list.Items = list.Items[:limit]
	}
	return list, nil
}

// deploymentViews une cada despliegue con su último lanzamiento. El actual es el del lanzamiento más reciente
// del ambiente.
func deploymentViews(deployments []Deployment, releases []Release) []DeploymentView {
	lastRelease := map[string]*Release{}
	for i := range releases {
		lastRelease[releases[i].DeploymentID] = &releases[i] // del más antiguo al más reciente: gana el último
	}
	currentDeployment := ""
	if len(releases) > 0 {
		currentDeployment = releases[len(releases)-1].DeploymentID
	}
	views := make([]DeploymentView, len(deployments))
	for i, d := range deployments {
		views[i] = DeploymentView{Deployment: d, Release: lastRelease[d.ID], Current: d.ID == currentDeployment}
	}
	sort.SliceStable(views, func(a, b int) bool { return views[a].Deployment.ID > views[b].Deployment.ID })
	return views
}

// ReleaseList son los lanzamientos de un ambiente, del más reciente al más antiguo.
type ReleaseList struct {
	Environment string
	Releases    []Release
	Total       int
}

// ListReleases devuelve los últimos limit lanzamientos del ambiente (todos si limit <= 0).
func (s *QueryService) ListReleases(ctx context.Context, environment string, limit int) (ReleaseList, error) {
	sess, err := s.open(ctx)
	if err != nil {
		return ReleaseList{}, err
	}
	if err := s.recents.CheckEnvironment(sess.projectID, environment); err != nil {
		return ReleaseList{}, err
	}
	releases, err := s.engine.Releases(ctx, sess.spec, environment)
	if err != nil {
		return ReleaseList{}, err
	}
	list := ReleaseList{Environment: environment, Total: len(releases), Releases: newestFirst(releases)}
	if limit > 0 && len(list.Releases) > limit {
		list.Releases = list.Releases[:limit]
	}
	return list, nil
}
