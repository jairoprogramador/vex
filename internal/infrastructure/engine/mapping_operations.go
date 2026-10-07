package engine

import (
	"time"

	"github.com/jairoprogramador/vex/internal/application"
	"github.com/jairoprogramador/vex/internal/infrastructure/engine/protocol"
)

// Capa anticorrupción de las operaciones de consulta y de lanzamiento (ver mapping.go para intentar y logs).

func toSummary(r protocol.ResumenDeIntento) application.AttemptSummary {
	return application.AttemptSummary{
		ID: r.Id, Environment: r.Ambiente, Requester: r.Solicitante, UntilStep: r.HastaPaso,
		StartedAt: r.Instante, Status: toAttemptStatus(r.Estado), Cause: toCause(r.Causa),
	}
}

func toSummaries(list []protocol.ResumenDeIntento) []application.AttemptSummary {
	summaries := make([]application.AttemptSummary, len(list))
	for i, r := range list {
		summaries[i] = toSummary(r)
	}
	return summaries
}

func toCause(c string) application.AttemptCause {
	switch c {
	case "error":
		return application.CauseError
	case "interrumpido":
		return application.CauseInterrupted
	}
	return application.AttemptCause(c)
}

// toAttemptDetail resume lo que el Historial guarda de un intento: cómo terminó y qué pasó en cada paso.
func toAttemptDetail(i protocol.IntentoDeHistorial) application.AttemptDetail {
	status := toAttemptStatus(i.Estado)
	detail := application.AttemptDetail{
		AttemptSummary: application.AttemptSummary{
			ID: i.Id, Environment: i.Apertura.Ambiente, Requester: i.Apertura.Solicitante,
			UntilStep: i.Apertura.HastaPaso, StartedAt: i.Instante, Status: status, Cause: toCause(i.Causa),
			Abandoned: i.Abandonado,
		},
		RolledBackTo: i.Destino,
		Steps:        stepOutcomes(i, status),
	}
	detail.Duration = lastRecordAt(i).Sub(i.Instante)
	return detail
}

// stepOutcomes recorre los pasos pedidos, en orden, y dice qué pasó en cada uno según su último registro.
func stepOutcomes(i protocol.IntentoDeHistorial, status application.AttemptStatus) []application.StepOutcome {
	var outcomes []application.StepOutcome
	for _, declared := range i.Apertura.Pasos {
		outcomes = append(outcomes, application.StepOutcome{
			Name: declared.Nombre, Status: stepStatusFromRecords(declared.Nombre, i.Registros, status),
		})
		if declared.Nombre == i.Apertura.HastaPaso {
			break
		}
	}
	return outcomes
}

func stepStatusFromRecords(step string, records []protocol.RegistroDePaso, attempt application.AttemptStatus) application.StepStatus {
	var last *protocol.RegistroDePaso
	for k := range records {
		if records[k].Paso == step {
			last = &records[k]
		}
	}
	switch {
	case last == nil:
		return application.StepPending
	case last.Tipo == protocol.RegistroNoReejecucion:
		return application.StepReused
	case last.Tipo == protocol.RegistroComienzo:
		return application.StepUnfinished
	case last.Exitoso:
		return application.StepExecuted
	case attempt == application.AttemptCanceled:
		// Un comando cancelado se registra como no exitoso; el desenlace del intento dice que fue la cancelación.
		return application.StepCanceled
	}
	return application.StepFailed
}

func lastRecordAt(i protocol.IntentoDeHistorial) time.Time {
	last := i.Instante
	for _, r := range i.Registros {
		if r.Instante.After(last) {
			last = r.Instante
		}
	}
	return last
}

func toDeployments(list []protocol.Despliegue) []application.Deployment {
	deployments := make([]application.Deployment, len(list))
	for i, d := range list {
		deployments[i] = application.Deployment{
			ID: d.Id, Environment: d.Ambiente, AttemptID: d.Intento, ParentID: d.Padre, At: d.Instante,
		}
	}
	return deployments
}

func toRelease(l protocol.Lanzamiento) application.Release {
	return application.Release{
		ID: l.Id, Environment: l.Ambiente, DeploymentID: l.Despliegue, Version: l.Version, Name: l.Nombre, At: l.Instante,
	}
}

func toReleases(list []protocol.Lanzamiento) []application.Release {
	releases := make([]application.Release, len(list))
	for i, l := range list {
		releases[i] = toRelease(l)
	}
	return releases
}

func toEnvironments(list []protocol.Ambiente) []application.Environment {
	environments := make([]application.Environment, len(list))
	for i, a := range list {
		environments[i] = application.Environment{
			Name: a.Nombre, Description: a.Descripcion, Value: a.Valor, Protected: a.Reservado,
		}
	}
	return environments
}

func toPipelineSteps(list []protocol.PasoDelPipeline) []application.PipelineStep {
	steps := make([]application.PipelineStep, len(list))
	for i, p := range list {
		steps[i] = application.PipelineStep{Name: p.Nombre, Order: p.Orden, Shared: p.Compartido}
	}
	return steps
}

func toCheckParams(req application.CheckRequest) protocol.PeticionDeSimulacion {
	return protocol.PeticionDeSimulacion{
		Version: protocol.LanguageVersion, Ambiente: req.Environment, Solicitante: req.Requester,
		HastaPaso: req.UntilStep, Fuente: req.Pipeline.Source, Commit: req.Pipeline.Commit,
		Metadatos: toMetadata(req.Project),
	}
}

func toMetadata(p application.ProjectMetadata) protocol.Metadatos {
	return protocol.Metadatos{
		ProjectId: p.ID, ProjectName: p.Name, ProjectOrganization: p.Organization, ProjectTeam: p.Team,
	}
}

func toCheckResult(r protocol.ResultadoDeSimulacion) application.CheckResult {
	result := application.CheckResult{Valid: r.Estado == protocol.EstadoExitoso}
	if r.Causa == nil {
		return result
	}
	for _, f := range r.Causa.Fallos {
		result.Failures = append(result.Failures, application.PipelineFailure{
			Invariant: f.Invariante, File: f.Fichero, Step: f.Paso, Detail: f.Detalle,
		})
	}
	for _, m := range r.Causa.Faltante {
		result.Missing = append(result.Missing, application.MissingVariables{Step: m.Paso, Variables: m.Variables})
	}
	return result
}

func toDiagnosis(r protocol.RespuestaDeDiagnostico) application.Diagnosis {
	switch r.SinDiagnostico {
	case protocol.SinReferencia:
		return application.Diagnosis{Kind: application.DiagnosisNoReference}
	case protocol.NoSeAtribuye:
		return application.Diagnosis{Kind: application.DiagnosisNotAttributable}
	}
	diagnosis := application.Diagnosis{Kind: application.DiagnosisFound, Environment: r.Ambiente}
	if r.IntentoExitoso != nil {
		diagnosis.Reference = application.DiagnosedAttempt{ID: r.IntentoExitoso.Id, At: r.IntentoExitoso.Fecha}
	}
	if r.IntentoFallido != nil {
		diagnosis.Failed = application.DiagnosedAttempt{ID: r.IntentoFallido.Id, At: r.IntentoFallido.Fecha}
		diagnosis.AttemptsSince = r.IntentoFallido.CantidadDeIntentos
	}
	if r.Sustento != nil {
		diagnosis.Changes = toChanges(*r.Sustento)
	}
	return diagnosis
}

func toChanges(s protocol.Sustento) application.DiagnosisChanges {
	var changes application.DiagnosisChanges
	if s.Codigo != nil {
		changes.Code = s.Codigo.Pasos
	}
	if s.Instrucciones != nil {
		changes.Instructions = s.Instrucciones.Pasos
	}
	if s.Variables != nil {
		changes.DeclaredVars = toVariableRefs(s.Variables.DeclaradasCambiadas)
		changes.ProducedVars = toVariableRefs(s.Variables.ProducidasCambiadas)
	}
	return changes
}

func toVariableRefs(list []protocol.VariableDeUnPaso) []application.VariableRef {
	refs := make([]application.VariableRef, len(list))
	for i, v := range list {
		refs[i] = application.VariableRef{Step: v.Paso, Name: v.Nombre}
	}
	return refs
}
