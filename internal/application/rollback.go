package application

import (
	"context"
	"errors"
	"time"

	proVos "github.com/jairoprogramador/vex/internal/domain/project/vos"
)

// ErrDeploymentRequired: se pidió un rollback sin decir a qué despliegue volver.
var ErrDeploymentRequired = errors.New("falta el despliegue al que volver")

// ErrRollbackEnvironmentUnknown: con las credenciales del portal hace falta saber de qué ambiente es el
// despliegue (la conexión de nube es por ambiente) y el CLI nunca lo vio.
var ErrRollbackEnvironmentUnknown = errors.New(
	"no sé de qué ambiente es ese despliegue y hace falta para usar su conexión de nube.\n→ " +
		"Ejecuta `vex deployments <ambiente>` para que vex lo conozca y repite el rollback")

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
	// portalCreds es la fuente de credenciales para cloud.credentials: portal; nil si no se inyectó.
	portalCreds CloudCredentials
	// preparer prepara los proyectos con cloud.credentials: portal; nil si no se inyectó.
	preparer *CloudPreparer
	// verifier comprueba la firma de la imagen de runtime de esos proyectos.
	verifier ImageVerifier
}

// RollbackOption personaliza el rollback.
type RollbackOption func(*RollbackService)

// WithRollbackPortalCredentials es WithPortalCredentials para el rollback.
func WithRollbackPortalCredentials(credentials CloudCredentials) RollbackOption {
	return func(s *RollbackService) { s.portalCreds = credentials }
}

// WithRollbackCloudPreparer es WithCloudPreparer para el rollback.
func WithRollbackCloudPreparer(preparer *CloudPreparer) RollbackOption {
	return func(s *RollbackService) { s.preparer = preparer }
}

// WithRollbackImageVerifier es WithImageVerifier para el rollback.
func WithRollbackImageVerifier(verifier ImageVerifier) RollbackOption {
	return func(s *RollbackService) { s.verifier = verifier }
}

func NewRollbackService(
	workspace *Workspace, engine rollbackEngine, presenter ExecutionPresenter, recents *RecentsService,
	options ...RollbackOption,
) *RollbackService {
	s := &RollbackService{workspace: workspace, engine: engine, presenter: presenter, recents: recents}
	for _, option := range options {
		option(s)
	}
	return s
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
	if err := s.workspace.Metadata(project).Validate(); err != nil {
		return err
	}
	if err := ValidateCloudConfig(project, proVos.NeutralReservedEnvPrefixes); err != nil {
		return err
	}
	credentials, err := selectCredentials(project, s.portalCreds)
	if err != nil {
		return err
	}
	if project.Cloud().IsPortal() && plan.Environment == "" {
		return ErrRollbackEnvironmentUnknown
	}
	preparation, err := beginCloud(ctx, s.preparer, project, nil)
	if err != nil {
		return err
	}
	defer preparation.Close()
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
	image, err := resolveRunImage(ctx, s.workspace, project, s.verifier)
	if err != nil {
		return err
	}
	// La conexión y los pasos de nube son los del pipeline de hoy, aunque el rollback ejecute el de entonces:
	// lo que se aceptó con `vex bootstrap` es lo vigente.
	cloud, err := readyCloud(ctx, preparation, pipelineSource, plan.Environment, s.presenter)
	if err != nil {
		return err
	}
	spec := s.workspace.ExecutionSpec(image, project, projectSource, pipelineSource)

	projectID := project.ID().String()
	tracker := &attemptTracker{recorder: s.recents, projectID: projectID, environment: plan.Environment}
	// Sin Commit: el rollback despliega los commits del despliegue anterior, que solo conoce el motor.
	result, err := withCloudSession(ctx, credentials, CloudSessionRequest{
		ProjectID: projectID, Environment: plan.Environment,
		CloudSteps: cloud.Bootstrap.Manifest.CloudSteps, Connection: cloud.connection(),
	}, s.presenter, func(ctx context.Context, session CloudSession, observe func(EngineEvent)) (AttemptResult, error) {
		return s.engine.Rollback(ctx, withSessionMounts(spec, session), RollbackRequest{
			DeploymentID: plan.DeploymentID,
			Requester:    s.workspace.Requester(),
			Project:      s.workspace.Metadata(project),
			Secrets:      mergeSecrets(s.workspace.Secrets(project), session.Env()),
			Context:      CloudExecutionContext(cloud),
		}, func(event EngineEvent) {
			observe(event)
			s.presenter.Event(event)
			tracker.onEvent(event)
		})
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
