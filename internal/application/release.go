package application

import (
	"context"
	"fmt"
)

// WrongEnvironmentError: se pidió lanzar en un ambiente un despliegue que se hizo en otro.
type WrongEnvironmentError struct {
	DeploymentID string
	Actual       string
	Requested    string
}

func (e *WrongEnvironmentError) Error() string {
	return fmt.Sprintf("el despliegue %s es de %s, no de %s", e.DeploymentID, e.Actual, e.Requested)
}

// releaseEngine es lo que los lanzamientos y las reservas necesitan del motor.
type releaseEngine interface {
	Release(ctx context.Context, spec ContainerSpec, req ReleaseRequest) (Release, error)
	Protect(ctx context.Context, spec ContainerSpec, environment string) error
	Unprotect(ctx context.Context, spec ContainerSpec, environment string) error
}

// ReleaseService responde `vex release`, `protect` y `unprotect`. Solo escribe en el historial: no clona nada.
type ReleaseService struct {
	workspace *Workspace
	engine    releaseEngine
	recents   *RecentsService
}

func NewReleaseService(workspace *Workspace, engine releaseEngine, recents *RecentsService) *ReleaseService {
	return &ReleaseService{workspace: workspace, engine: engine, recents: recents}
}

// Release hace visible un despliegue en el ambiente. ref es el id completo o su final; name, opcional, el nombre
// de la versión.
//
// El motor no comprueba que el despliegue sea del ambiente pedido, así que el CLI lo hace con lo que recuerda del
// despliegue: es mejor frenar un `vex release prod <despliegue de sand>` que publicarlo.
func (s *ReleaseService) Release(ctx context.Context, environment, ref, name string) (Release, error) {
	history, err := s.workspace.OpenHistory(ctx)
	if err != nil {
		return Release{}, err
	}
	if err := s.recents.CheckEnvironment(history.ProjectID, environment); err != nil {
		return Release{}, err
	}
	id, err := s.recents.ResolveDeployment(history.ProjectID, ref)
	if err != nil {
		return Release{}, err
	}
	if known, found, err := s.recents.FindDeployment(history.ProjectID, id); err != nil {
		return Release{}, err
	} else if found && known.Environment != "" && known.Environment != environment {
		return Release{}, &WrongEnvironmentError{DeploymentID: id, Actual: known.Environment, Requested: environment}
	}
	return s.engine.Release(ctx, history.Spec, ReleaseRequest{Environment: environment, DeploymentID: id, Name: name})
}

// Protect reserva el ambiente: sus despliegues dejan de lanzarse solos.
func (s *ReleaseService) Protect(ctx context.Context, environment string) error {
	return s.setProtection(ctx, environment, s.engine.Protect)
}

// Unprotect devuelve el ambiente al lanzamiento automático.
func (s *ReleaseService) Unprotect(ctx context.Context, environment string) error {
	return s.setProtection(ctx, environment, s.engine.Unprotect)
}

// setProtection valida el ambiente antes de escribir: el motor aceptaría reservar uno que no existe.
func (s *ReleaseService) setProtection(ctx context.Context, environment string, apply func(context.Context, ContainerSpec, string) error) error {
	history, err := s.workspace.OpenHistory(ctx)
	if err != nil {
		return err
	}
	if err := s.recents.CheckEnvironment(history.ProjectID, environment); err != nil {
		return err
	}
	return apply(ctx, history.Spec, environment)
}
