package application

import (
	"context"
	"errors"
	"time"
)

// ErrDeploymentRequired: se pidió un rollback sin decir a qué despliegue volver.
var ErrDeploymentRequired = errors.New("falta el despliegue al que volver")

// rollbackEngine es lo que el rollback necesita del motor.
type rollbackEngine interface {
	Rollback(ctx context.Context, spec ContainerSpec, req RollbackRequest, onEvent func(EngineEvent)) (AttemptResult, error)
	Logs(ctx context.Context, spec ContainerSpec, req LogsRequest) ([]CommandOutput, error)
}

// RollbackPlan es lo que va a pasar si se confirma: a qué despliegue se vuelve y lo que se sabe de él. El
// ambiente y la fecha pueden estar vacíos si el CLI nunca vio ese despliegue (el motor los sabe igualmente).
type RollbackPlan struct {
	DeploymentID string
	Environment  string
	DeployedAt   time.Time
}

// RollbackService vuelve a desplegar los mismos commits de un despliegue anterior: el motor ejecuta TODOS los pasos
// del pipeline sobre el proyecto y el pipeline tal como estaban entonces, y el despliegue nuevo cuelga del anterior.
type RollbackService struct {
	workspace *Workspace
	engine    rollbackEngine
	presenter ExecutionPresenter
	recents   *RecentsService
}

func NewRollbackService(workspace *Workspace, engine rollbackEngine, presenter ExecutionPresenter, recents *RecentsService) *RollbackService {
	return &RollbackService{workspace: workspace, engine: engine, presenter: presenter, recents: recents}
}

// Plan resuelve a qué despliegue se volvería, sin ejecutar nada: quien llama lo muestra y pide confirmación antes
// de Run. ref es el id completo o su final (mínimo 6 caracteres), tal como sale en `vex deployments`.
func (s *RollbackService) Plan(ref string) (RollbackPlan, error) {
	if ref == "" {
		return RollbackPlan{}, ErrDeploymentRequired
	}
	project, err := s.workspace.LoadProject() // falla antes de preguntar nada si el proyecto no está inicializado
	if err != nil {
		return RollbackPlan{}, err
	}
	projectID := project.ID().String()
	id, err := s.recents.ResolveDeployment(projectID, ref)
	if err != nil {
		return RollbackPlan{}, err
	}
	plan := RollbackPlan{DeploymentID: id}
	if known, found, err := s.recents.FindDeployment(projectID, id); err == nil && found {
		plan.Environment, plan.DeployedAt = known.Environment, known.At
	}
	return plan, nil
}

// Run ejecuta el rollback del plan. El avance sale en vivo, igual que en un intento.
func (s *RollbackService) Run(ctx context.Context, plan RollbackPlan) error {
	return cancelAware(ctx, s.run(ctx, plan))
}

func (s *RollbackService) run(ctx context.Context, plan RollbackPlan) error {
	project, err := s.workspace.LoadProject()
	if err != nil {
		return err
	}
	if err := s.workspace.PrepareDirs(); err != nil {
		return err
	}
	// El motor reabre los repos en las mismas rutas del contenedor con que se hizo el despliegue: hacen falta los
	// dos clones, con su historia completa, aunque solo se vaya a leer un commit antiguo.
	projectSource, err := s.workspace.CloneProject(ctx, project)
	if err != nil {
		return err
	}
	pipelineSource, err := s.workspace.ClonePipeline(ctx, project)
	if err != nil {
		return err
	}
	image, err := s.workspace.ResolveImage(ctx, project)
	if err != nil {
		return err
	}
	spec := s.workspace.ExecutionSpec(image, project, projectSource, pipelineSource)

	projectID := project.ID().String()
	tracker := &attemptTracker{recorder: s.recents, projectID: projectID, environment: plan.Environment}
	result, err := s.engine.Rollback(ctx, spec, RollbackRequest{
		DeploymentID: plan.DeploymentID,
		Requester:    s.workspace.Requester(),
		Project:      s.workspace.Metadata(project),
		Secrets:      s.workspace.Secrets(project),
	}, func(event EngineEvent) {
		s.presenter.Event(event)
		tracker.onEvent(event)
	})
	if err != nil {
		tracker.onError(ctx, err)
		return asCanceledIfCanceled(err)
	}
	tracker.onResult(result)
	s.rememberNewDeployment(projectID, plan, result)
	s.presenter.Result(result)
	return reportOutcome(ctx, s.engine, s.presenter, spec, result)
}

// rememberNewDeployment recuerda el despliegue que acaba de nacer del rollback, para poder lanzarlo enseguida
// con un id corto. Sin saber el ambiente no se recuerda: el CLI no lo ha visto.
func (s *RollbackService) rememberNewDeployment(projectID string, plan RollbackPlan, result AttemptResult) {
	if result.DeploymentID == "" || plan.Environment == "" {
		return
	}
	_ = s.recents.RememberDeployments(projectID, []Deployment{
		{ID: result.DeploymentID, Environment: plan.Environment, AttemptID: result.AttemptID, ParentID: plan.DeploymentID, At: time.Now()},
	})
}
