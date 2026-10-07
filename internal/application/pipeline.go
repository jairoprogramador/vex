package application

import "context"

// PipelineService responde lo que se puede saber del pipeline sin ejecutarlo: sus ambientes, sus pasos y si un
// paso está listo para ejecutarse en un ambiente. Necesita el clon del pipeline y el historial (la reserva de
// cada ambiente vive allí), pero no el proyecto.
type PipelineService struct {
	workspace *Workspace
	engine    pipelineEngine
	guard     *PipelineGuard
	recents   *RecentsService
}

func NewPipelineService(workspace *Workspace, engine pipelineEngine, guard *PipelineGuard, recents *RecentsService) *PipelineService {
	return &PipelineService{workspace: workspace, engine: engine, guard: guard, recents: recents}
}

type pipelineSession struct {
	projectID string
	spec      ContainerSpec
	pipeline  PipelineRef
	metadata  ProjectMetadata
}

func (s *PipelineService) open(ctx context.Context) (pipelineSession, error) {
	project, err := s.workspace.LoadProject()
	if err != nil {
		return pipelineSession{}, err
	}
	if err := s.workspace.PrepareDirs(); err != nil {
		return pipelineSession{}, err
	}
	source, err := s.workspace.ClonePipeline(ctx, project)
	if err != nil {
		return pipelineSession{}, err
	}
	image, err := s.workspace.ResolveImage(ctx, project)
	if err != nil {
		return pipelineSession{}, err
	}
	return pipelineSession{
		projectID: project.ID().String(),
		spec:      s.workspace.PipelineSpec(image, source),
		pipeline:  s.workspace.PipelineRef(source),
		metadata:  s.workspace.Metadata(project),
	}, nil
}

// Environments son los ambientes del pipeline, en su orden, y cuáles están protegidos. Lo que averigua se
// recuerda: los comandos que no clonan lo usan para avisar de un ambiente mal tecleado.
func (s *PipelineService) Environments(ctx context.Context) ([]Environment, error) {
	sess, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	environments, err := s.engine.Environments(ctx, sess.spec, sess.pipeline)
	if err != nil {
		return nil, err
	}
	_ = s.recents.RememberEnvironments(sess.projectID, environments)
	return environments, nil
}

// Steps son los pasos del pipeline, en su orden.
func (s *PipelineService) Steps(ctx context.Context) ([]PipelineStep, error) {
	sess, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	steps, err := s.engine.Steps(ctx, sess.spec, sess.pipeline)
	if err != nil {
		return nil, err
	}
	_ = s.recents.RememberSteps(sess.projectID, steps)
	return steps, nil
}

// Check comprueba, sin ejecutar nada, que el pipeline es válido y que el paso se puede ejecutar en el ambiente.
// Devuelve nil si todo está en orden; si no, el error que explica qué falta (ver PipelineGuard).
func (s *PipelineService) Check(ctx context.Context, step, environment string) error {
	if environment == "" {
		return ErrEnvironmentRequired
	}
	sess, err := s.open(ctx)
	if err != nil {
		return err
	}
	return s.guard.Check(ctx, sess.spec, sess.projectID, CheckRequest{
		Environment: environment, Requester: s.workspace.Requester(), UntilStep: step,
		Pipeline: sess.pipeline, Project: sess.metadata,
	})
}
