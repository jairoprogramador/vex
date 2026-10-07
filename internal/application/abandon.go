package application

import (
	"context"
	"errors"
	"fmt"
)

// ErrNoOpenAttempt: se pidió abandonar «el intento atascado» y no se recuerda ninguno sin terminar.
var ErrNoOpenAttempt = errors.New("no recuerdo ningún intento sin terminar en este proyecto")

// OpenAttemptsError: hay varios intentos sin terminar y no se sabe cuál abandonar. Abandonar uno equivocado
// liberaría un ambiente que alguien está usando, así que no se adivina.
type OpenAttemptsError struct {
	Candidates []RecentAttempt
}

func (e *OpenAttemptsError) Error() string {
	return fmt.Sprintf("hay %d intentos sin terminar: indica cuál abandonar", len(e.Candidates))
}

// AttemptFinishedError: el intento ya terminó (o ya se abandonó): no ocupa nada que liberar.
type AttemptFinishedError struct {
	AttemptID   string
	Environment string
	Status      AttemptStatus
	Abandoned   bool
}

func (e *AttemptFinishedError) Error() string {
	return fmt.Sprintf("el intento %s ya terminó, no hay nada que abandonar", e.AttemptID)
}

// abandonEngine es lo que abandonar necesita del motor.
type abandonEngine interface {
	AttemptDetail(ctx context.Context, spec ContainerSpec, attemptID string) (AttemptDetail, error)
	Abandon(ctx context.Context, spec ContainerSpec, attemptID string) error
}

// AbandonPlan es lo que va a pasar si se confirma: qué intento se abandona y de qué ambiente.
type AbandonPlan struct {
	Attempt AttemptDetail
	session HistorySession
}

// AbandonService libera un ambiente que un intento muerto dejó ocupado. Normalmente no hace falta: el motor
// recupera solo el ambiente de un proceso caído; esto es para quien no quiere esperar.
type AbandonService struct {
	workspace *Workspace
	engine    abandonEngine
	recents   *RecentsService
}

func NewAbandonService(workspace *Workspace, engine abandonEngine, recents *RecentsService) *AbandonService {
	return &AbandonService{workspace: workspace, engine: engine, recents: recents}
}

// Plan averigua qué intento se abandonaría, comprobando con el motor que de verdad no terminó. No cambia nada:
// quien llama pide confirmación y después llama a Abandon con el plan. ref es el id completo o su final; sin
// ref se usa el único intento sin terminar que se recuerda.
func (s *AbandonService) Plan(ctx context.Context, ref string) (AbandonPlan, error) {
	history, err := s.workspace.OpenHistory(ctx)
	if err != nil {
		return AbandonPlan{}, err
	}
	id, err := s.resolve(history.ProjectID, ref)
	if err != nil {
		return AbandonPlan{}, err
	}
	detail, err := s.engine.AttemptDetail(ctx, history.Spec, id)
	if err != nil {
		return AbandonPlan{}, err
	}
	if !detail.InProgress() || detail.Abandoned {
		return AbandonPlan{}, &AttemptFinishedError{
			AttemptID: detail.ID, Environment: detail.Environment, Status: detail.Status, Abandoned: detail.Abandoned,
		}
	}
	return AbandonPlan{Attempt: detail, session: history}, nil
}

func (s *AbandonService) resolve(projectID, ref string) (string, error) {
	if ref != "" {
		return s.recents.ResolveAttempt(projectID, ref)
	}
	open, err := s.recents.OpenAttempts(projectID)
	if err != nil {
		return "", err
	}
	switch len(open) {
	case 0:
		return "", ErrNoOpenAttempt
	case 1:
		return open[0].ID, nil
	}
	return "", &OpenAttemptsError{Candidates: open}
}

// Abandon abandona el intento del plan y libera su ambiente. No detiene los comandos que su proceso siga
// ejecutando, si es que sigue vivo.
func (s *AbandonService) Abandon(ctx context.Context, plan AbandonPlan) error {
	if err := s.engine.Abandon(ctx, plan.session.Spec, plan.Attempt.ID); err != nil {
		return err
	}
	_ = s.recents.RememberAttempt(plan.session.ProjectID, RecentAttempt{ID: plan.Attempt.ID, Abandoned: true})
	return nil
}
