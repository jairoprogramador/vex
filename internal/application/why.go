package application

import (
	"context"
	"errors"
)

// ErrNoFailedAttempt: se pidió «por qué falló lo último» y no se recuerda ningún intento fallido.
var ErrNoFailedAttempt = errors.New("no hay ningún intento fallido reciente en este proyecto")

// WhyOutcome es lo que `vex why` puede responder.
type WhyOutcome string

const (
	// WhyDiagnosed: hay una comparación con la última vez que funcionó (Diagnosis).
	WhyDiagnosed WhyOutcome = "diagnosed"
	// WhyNoReference: no hay una ejecución anterior que funcionara con la que comparar.
	WhyNoReference WhyOutcome = "no_reference"
	// WhyNotAFailure: el intento salió bien.
	WhyNotAFailure WhyOutcome = "not_a_failure"
	// WhyCanceled, WhyInterrupted, WhyInProgress: el intento no terminó por un fallo, y no falló por ningún
	// cambio del pipeline: no hay nada que diagnosticar.
	WhyCanceled    WhyOutcome = "canceled"
	WhyInterrupted WhyOutcome = "interrupted"
	WhyInProgress  WhyOutcome = "in_progress"
	// WhyNotAttributable: el motor no atribuye causa a este intento por una razón que el CLI no distingue.
	WhyNotAttributable WhyOutcome = "not_attributable"
)

// WhyReport es la respuesta de `vex why`.
type WhyReport struct {
	AttemptID   string
	Environment string
	Outcome     WhyOutcome
	// Diagnosis solo se rellena con WhyDiagnosed.
	Diagnosis Diagnosis
}

// diagnosisEngine es lo que el diagnóstico necesita del motor.
type diagnosisEngine interface {
	AttemptDetail(ctx context.Context, spec ContainerSpec, attemptID string) (AttemptDetail, error)
	Diagnose(ctx context.Context, spec ContainerSpec, req DiagnoseRequest) (Diagnosis, error)
}

// DiagnosisService responde `vex why`: qué cambió desde la última vez que algo funcionó. Solo lee el historial.
type DiagnosisService struct {
	workspace *Workspace
	engine    diagnosisEngine
	recents   *RecentsService
}

func NewDiagnosisService(workspace *Workspace, engine diagnosisEngine, recents *RecentsService) *DiagnosisService {
	return &DiagnosisService{workspace: workspace, engine: engine, recents: recents}
}

// Why diagnostica un intento. ref es el id completo o su final; vacío es el último intento fallido que se
// recuerda. againstDeployment, opcional, elige con qué despliegue comparar (por defecto, el último anterior del
// mismo ambiente).
//
// Solo se le pide diagnóstico al motor si el intento falló por sus comandos: uno que salió bien, se canceló, se
// interrumpió o sigue en curso no tiene qué diagnosticar, y comparar éxito contra éxito no diría nada.
func (s *DiagnosisService) Why(ctx context.Context, ref, againstDeployment string) (WhyReport, error) {
	history, err := s.workspace.OpenHistory(ctx)
	if err != nil {
		return WhyReport{}, err
	}
	id, err := s.resolveAttempt(history.ProjectID, ref)
	if err != nil {
		return WhyReport{}, err
	}
	reference, err := s.resolveReference(history.ProjectID, againstDeployment)
	if err != nil {
		return WhyReport{}, err
	}

	state, err := s.stateOf(ctx, history, id)
	if err != nil {
		return WhyReport{}, err
	}
	report := WhyReport{AttemptID: id, Environment: state.Environment}
	if outcome, final := outcomeWithoutDiagnosis(state); final {
		report.Outcome = outcome
		return report, nil
	}

	diagnosis, err := s.engine.Diagnose(ctx, history.Spec, DiagnoseRequest{AttemptID: id, ReferenceDeploymentID: reference})
	if err != nil {
		return WhyReport{}, err
	}
	switch diagnosis.Kind {
	case DiagnosisFound:
		report.Outcome, report.Diagnosis = WhyDiagnosed, diagnosis
		if diagnosis.Environment != "" {
			report.Environment = diagnosis.Environment
		}
	case DiagnosisNoReference:
		report.Outcome = WhyNoReference
	default:
		report.Outcome = WhyNotAttributable
	}
	return report, nil
}

func (s *DiagnosisService) resolveAttempt(projectID, ref string) (string, error) {
	if ref != "" {
		return s.recents.ResolveAttempt(projectID, ref)
	}
	last, ok, err := s.recents.LastAttempt(projectID, true)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrNoFailedAttempt
	}
	return last.ID, nil
}

func (s *DiagnosisService) resolveReference(projectID, ref string) (string, error) {
	if ref == "" {
		return "", nil
	}
	return s.recents.ResolveDeployment(projectID, ref)
}

// attemptState es lo que hace falta saber de un intento para decidir si tiene algo que diagnosticar.
type attemptState struct {
	Environment string
	Status      AttemptStatus
	Cause       AttemptCause
	Abandoned   bool
}

// stateOf usa lo que se recuerda del intento y, si no se sabe cómo terminó, se lo pregunta al motor.
func (s *DiagnosisService) stateOf(ctx context.Context, history HistorySession, id string) (attemptState, error) {
	known, found, err := s.recents.FindAttempt(history.ProjectID, id)
	if err != nil {
		return attemptState{}, err
	}
	if found && known.Status != "" {
		return attemptState{Environment: known.Environment, Status: known.Status, Cause: known.Cause}, nil
	}

	detail, err := s.engine.AttemptDetail(ctx, history.Spec, id)
	if err != nil {
		return attemptState{}, err
	}
	_ = s.recents.RememberAttempt(history.ProjectID, RecentAttempt{
		ID: detail.ID, Environment: detail.Environment, UntilStep: detail.UntilStep,
		Status: detail.Status, Cause: detail.Cause, At: detail.StartedAt, Abandoned: detail.Abandoned,
	})
	return attemptState{
		Environment: detail.Environment, Status: detail.Status, Cause: detail.Cause, Abandoned: detail.Abandoned,
	}, nil
}

// outcomeWithoutDiagnosis dice si el intento ya se explica solo, sin preguntarle al motor por una comparación.
func outcomeWithoutDiagnosis(state attemptState) (WhyOutcome, bool) {
	switch {
	case state.Abandoned:
		return WhyNotAttributable, true
	case state.Status == AttemptSucceeded:
		return WhyNotAFailure, true
	case state.Status == AttemptCanceled:
		return WhyCanceled, true
	case state.Status == "":
		return WhyInProgress, true
	case state.Cause == CauseInterrupted:
		return WhyInterrupted, true
	}
	return "", false
}
