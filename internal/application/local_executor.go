package application

import (
	"context"
	"errors"
	"time"

	"github.com/jairoprogramador/vex/internal/domain/project/aggregates"
	proVos "github.com/jairoprogramador/vex/internal/domain/project/vos"
)

// localEngine es lo que el ejecutor necesita del motor: pedir un intento y leer la salida de los comandos fallidos.
type localEngine interface {
	Attempt(ctx context.Context, spec ContainerSpec, req AttemptRequest, onEvent func(EngineEvent)) (AttemptResult, error)
	Logs(ctx context.Context, spec ContainerSpec, req LogsRequest) ([]CommandOutput, error)
}

// AttemptRecorder recuerda los intentos que el CLI ejecuta, para comandos como `vex why` sin argumentos.
type AttemptRecorder interface {
	RememberAttempt(projectID string, attempt RecentAttempt) error
}

// LocalExecutorOption personaliza el ejecutor.
type LocalExecutorOption func(*LocalExecutorService)

// WithAttemptRecorder hace que el ejecutor recuerde cada intento que lanza.
func WithAttemptRecorder(recorder AttemptRecorder) LocalExecutorOption {
	return func(s *LocalExecutorService) { s.recorder = recorder }
}

// WithErrorExplainer hace que un ambiente o un paso inexistente, cuando el motor lo rechaza, se explique con los
// nombres que sí existen y una sugerencia. No cuesta nada en el camino feliz: solo pregunta al motor al fallar.
func WithErrorExplainer(guard *PipelineGuard) LocalExecutorOption {
	return func(s *LocalExecutorService) { s.explainer = guard }
}

// WithPreflight hace que, antes de abrir un intento, el ejecutor compruebe con el guardián que el paso se puede
// ejecutar (ambiente, paso y variables), sin ejecutar nada. Cuesta un contenedor más por ejecución. También
// explica los errores, como WithErrorExplainer.
func WithPreflight(guard *PipelineGuard) LocalExecutorOption {
	return func(s *LocalExecutorService) {
		s.guard = guard
		if s.explainer == nil {
			s.explainer = guard
		}
	}
}

// WithPortalCredentials inyecta la fuente de credenciales de nube que se usa cuando el proyecto declara
// cloud.credentials: portal. Sin ella, un proyecto así no se puede ejecutar.
func WithPortalCredentials(credentials CloudCredentials) LocalExecutorOption {
	return func(s *LocalExecutorService) { s.portalCreds = credentials }
}

// WithCloudPreparer inyecta la preparación de los proyectos con cloud.credentials: portal: sesión de vex,
// proyecto en el portal y conexión de nube comprobada antes de crear ningún contenedor.
func WithCloudPreparer(preparer *CloudPreparer) LocalExecutorOption {
	return func(s *LocalExecutorService) { s.preparer = preparer }
}

// WithImageVerifier inyecta quien verifica la firma de la imagen antes de que reciba credenciales del portal.
// Sin él, un proyecto con cloud.credentials: portal y una imagen de registro no se ejecuta.
func WithImageVerifier(verifier ImageVerifier) LocalExecutorOption {
	return func(s *LocalExecutorService) { s.verifier = verifier }
}

// WithTimingsFile hace que cada ejecución añada sus tiempos a ese archivo (ver AppendTimings).
func WithTimingsFile(path string) LocalExecutorOption {
	return func(s *LocalExecutorService) { s.timingsFile = path }
}

// LocalExecutorService ejecuta un paso del pipeline con vex-engine en un contenedor
// local: clona proyecto y pipeline, los monta y le pide un intento al motor.
type LocalExecutorService struct {
	workspace *Workspace
	engine    localEngine
	presenter ExecutionPresenter
	recorder  AttemptRecorder
	guard     *PipelineGuard // pre-vuelo: nil si no se comprueba antes de ejecutar
	explainer *PipelineGuard // explica los errores de ambiente/paso: nil si no se explican
	// portalCreds es la fuente de credenciales para cloud.credentials: portal; nil si no se inyectó.
	portalCreds CloudCredentials
	// preparer prepara los proyectos con cloud.credentials: portal; nil si no se inyectó.
	preparer *CloudPreparer
	// verifier comprueba la firma de la imagen de runtime de esos proyectos.
	verifier ImageVerifier
	// timingsFile es donde se guardan los tiempos de cada ejecución; "" si no se guardan.
	timingsFile string
	now         func() time.Time
}

func NewLocalExecutorService(
	workspace *Workspace,
	engine localEngine,
	presenter ExecutionPresenter,
	options ...LocalExecutorOption,
) *LocalExecutorService {
	s := &LocalExecutorService{
		workspace: workspace,
		engine:    engine,
		presenter: presenter,
		now:       time.Now,
	}
	for _, option := range options {
		option(s)
	}
	return s
}

var _ Runner = (*LocalExecutorService)(nil)

func (s *LocalExecutorService) Run(ctx context.Context, step, environment string) error {
	if environment == "" {
		return ErrEnvironmentRequired
	}
	if err := ValidateEngineEnvironment(environment); err != nil {
		return err
	}

	return cancelAware(ctx, s.run(ctx, step, environment))
}

func (s *LocalExecutorService) run(ctx context.Context, step, environment string) error {
	timer := NewPhaseTimer(s.now)
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
	// Antes de clonar: el portal puede corregir de qué pipeline se trata. Sin portal no se le llama nunca.
	preparation, err := beginCloud(ctx, s.preparer, project, timer)
	if err != nil {
		return err
	}
	defer preparation.Close()
	if err := s.workspace.PrepareDirs(); err != nil {
		return err
	}

	projectSource, err := s.workspace.CloneProject(ctx, project)
	if err != nil {
		return err
	}
	pipelineSource, err := s.workspace.ClonePipeline(ctx, project)
	if err != nil {
		return err
	}

	// Antes del primer contenedor y de abrir la sesión de nube: una imagen sin verificar no recibe credenciales.
	stopVerify := timer.Phase("imagen")
	image, err := resolveRunImage(ctx, s.workspace, project, s.verifier)
	stopVerify()
	if err != nil {
		return err
	}
	// Antes del primer contenedor: sin conexión de nube utilizable, la ejecución termina aquí.
	plan, err := readyCloud(ctx, preparation, pipelineSource, environment, s.presenter)
	if err != nil {
		return err
	}

	spec := s.workspace.ExecutionSpec(image, project, projectSource, pipelineSource)
	stopPreflight := timer.Phase("pre_vuelo")
	err = s.preflight(ctx, spec, project.ID().String(), step, environment, pipelineSource, project)
	stopPreflight()
	if err != nil {
		return err
	}

	request := s.attemptRequest(project, step, environment, projectSource, pipelineSource)
	request.Context = CloudExecutionContext(plan)
	tracker := &attemptTracker{
		recorder: s.recorder, projectID: project.ID().String(), environment: environment, step: step,
	}
	// La sesión se abre lo más tarde posible (si el pre-vuelo rechaza el paso, no hay nada que limpiar) y solo
	// el intento lleva su montaje: explicar un error o leer la salida no la necesitan.
	stopEngine := timer.Phase("motor")
	result, err := withCloudSession(ctx, credentials, CloudSessionRequest{
		ProjectID: project.ID().String(), Environment: environment, Commit: projectSource.Commit,
		CloudSteps: plan.Bootstrap.Manifest.CloudSteps, Connection: plan.connection(),
	}, s.presenter, func(ctx context.Context, session CloudSession, observe func(EngineEvent)) (AttemptResult, error) {
		request.Secrets = mergeSecrets(request.Secrets, session.Env())
		return s.engine.Attempt(ctx, withSessionMounts(spec, session), request, func(event EngineEvent) {
			observe(event)
			timer.Observe(event)
			s.presenter.Event(event)
			tracker.onEvent(event)
		})
	})
	stopEngine()
	if err != nil {
		tracker.onError(ctx, err)
		err = s.explain(ctx, spec, project.ID().String(), pipelineSource, err)
		return withEnvironment(asCanceledIfCanceled(err), environment)
	}
	tracker.onResult(result)
	result.Elapsed = timer.Elapsed()
	s.recordTimings(timer, project.ID().String(), environment, step, result.Status)
	s.presenter.Result(result)
	return s.outcome(ctx, spec, result)
}

// recordTimings guarda los tiempos de la ejecución. Es de mejor esfuerzo: medir no puede hacer fallar un intento.
func (s *LocalExecutorService) recordTimings(timer *PhaseTimer, projectID, environment, step string, status AttemptStatus) {
	if s.timingsFile == "" {
		return
	}
	run := timer.Timings()
	run.Project, run.Environment, run.Step, run.Status = projectID, environment, step, status
	_ = AppendTimings(s.timingsFile, run)
}

// explain traduce un ambiente o paso rechazado por el motor al error que dice cuáles existen.
func (s *LocalExecutorService) explain(ctx context.Context, spec ContainerSpec, projectID string, pipeline Source, err error) error {
	if s.explainer == nil {
		return err
	}
	return s.explainer.Explain(ctx, spec, projectID, s.workspace.PipelineRef(pipeline), err)
}

// preflight comprueba el paso antes de abrir el intento. Sin guardián (--no-check) no hace nada.
func (s *LocalExecutorService) preflight(
	ctx context.Context, spec ContainerSpec, projectID, step, environment string, pipeline Source, project *aggregates.Project,
) error {
	if s.guard == nil {
		return nil
	}
	return s.guard.Check(ctx, spec, projectID, CheckRequest{
		Environment: environment, Requester: s.workspace.Requester(), UntilStep: step,
		Pipeline: s.workspace.PipelineRef(pipeline), Project: s.workspace.Metadata(project),
	})
}

func (s *LocalExecutorService) attemptRequest(
	project *aggregates.Project, step, environment string, projectSource, pipelineSource Source,
) AttemptRequest {
	return AttemptRequest{
		Environment:    environment,
		Requester:      s.workspace.Requester(),
		ProjectSource:  containerProjectPath,
		ProjectCommit:  projectSource.Commit,
		PipelineSource: containerPipelinePath,
		PipelineCommit: pipelineSource.Commit,
		UntilStep:      step,
		Project:        s.workspace.Metadata(project),
		Secrets:        s.workspace.Secrets(project),
	}
}

// outcome convierte el resultado del intento en el error que ve quien llama.
func (s *LocalExecutorService) outcome(ctx context.Context, spec ContainerSpec, result AttemptResult) error {
	return reportOutcome(ctx, s.engine, s.presenter, spec, result)
}

// withEnvironment completa el ambiente pedido en un error del motor que no lo
// trae, para que el mensaje pueda nombrarlo.
func withEnvironment(err error, environment string) error {
	var engineErr *EngineError
	if errors.As(err, &engineErr) && engineErr.Environment == "" {
		engineErr.Environment = environment
	}
	return err
}

func asCanceledIfCanceled(err error) error {
	var engineErr *EngineError
	if errors.As(err, &engineErr) && engineErr.Kind == EngineCanceled {
		return ErrAttemptCanceled
	}
	return err
}

// attemptTracker lleva la cuenta de un intento en la memoria del CLI. Es de mejor esfuerzo: si no se puede
// guardar, la ejecución sigue igual.
type attemptTracker struct {
	recorder    AttemptRecorder
	projectID   string
	environment string
	step        string
	id          string
}

func (t *attemptTracker) remember(attempt RecentAttempt) {
	if t.recorder == nil || t.id == "" {
		return
	}
	attempt.ID = t.id
	_ = t.recorder.RememberAttempt(t.projectID, attempt)
}

// onEvent recuerda el intento en cuanto el motor lo abre: así `vex why` lo encuentra aunque el proceso se
// interrumpa antes de terminar.
func (t *attemptTracker) onEvent(event EngineEvent) {
	if event.Kind != AttemptStarted {
		return
	}
	t.id = event.AttemptID
	t.remember(RecentAttempt{Environment: t.environment, UntilStep: t.step, At: time.Now()})
}

func (t *attemptTracker) onResult(result AttemptResult) {
	t.remember(RecentAttempt{Status: result.Status})
}

// onError: si el intento ya estaba abierto, el motor lo cierra como fallido ante cualquier error (o como
// cancelado si fue una cancelación). Si el contenedor murió sin responder no se sabe cómo acabó.
func (t *attemptTracker) onError(ctx context.Context, err error) {
	var engineErr *EngineError
	var accessLost *CloudAccessLostError
	switch {
	case errors.As(err, &engineErr) && engineErr.Kind == EngineDidNotRespond:
		return
	case errors.As(err, &accessLost): // la CLI canceló el intento
		t.remember(RecentAttempt{Status: AttemptCanceled})
	case ctx.Err() != nil || (engineErr != nil && engineErr.Kind == EngineCanceled):
		t.remember(RecentAttempt{Status: AttemptCanceled})
	default:
		t.remember(RecentAttempt{Status: AttemptFailed, Cause: CauseError})
	}
}
