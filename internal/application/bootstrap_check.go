package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrCheckFoundProblems: la auditoría de la conexión encontró algo que no está como vex lo dejó.
var ErrCheckFoundProblems = errors.New("la conexión de nube no pasó la auditoría")

// CheckStatus es el resultado de un punto de la auditoría.
type CheckStatus int

const (
	CheckOK CheckStatus = iota
	CheckFailed
	// CheckWarning: no se pudo comprobar del todo; no hace fallar la auditoría, pero se dice.
	CheckWarning
)

// CheckItem es un punto de la auditoría.
type CheckItem struct {
	Status CheckStatus
	Text   string
}

// CheckReport es la auditoría de la conexión de un ambiente, punto por punto.
type CheckReport struct {
	Environment string
	Contract    ContractID
	Account     []LabeledValue
	Items       []CheckItem
}

// Failed dice si algún punto falló.
func (r CheckReport) Failed() bool {
	for _, item := range r.Items {
		if item.Status == CheckFailed {
			return true
		}
	}
	return false
}

func (r *CheckReport) add(ok bool, pass, fail string) {
	if ok {
		r.Items = append(r.Items, CheckItem{Status: CheckOK, Text: pass})
		return
	}
	r.Items = append(r.Items, CheckItem{Status: CheckFailed, Text: fail})
}

// Check audita la conexión de un ambiente sin cambiar nada en la nube: que la sesión apunte a su destino, que
// el bootstrap aceptado sea el del pipeline, que la identidad solo confíe en vex, que aplicar el bootstrap otra
// vez no cambiaría nada y que el canje de un token real funcione. Devuelve el informe aunque haya fallos; el
// error es para lo que impide auditar.
func (s *BootstrapService) Check(ctx context.Context, environment string) (CheckReport, error) {
	setup, err := s.open(ctx, environment)
	if err != nil {
		return CheckReport{}, err
	}
	conn := setup.conn
	if conn.Status != ConnectionActive || conn.Outputs == nil {
		return CheckReport{}, &ConnectionNotReadyError{Environment: environment, Status: conn.Status, Hint: conn.Hint}
	}
	report := CheckReport{Environment: environment, Contract: conn.Outputs.ContractID, Account: setup.account.Summary}
	again := "Ejecuta `vex bootstrap " + environment + "`"

	report.add(conn.BootstrapSHA256 == setup.bootstrap.Fingerprint && conn.Outputs.ContractID == setup.bootstrap.Manifest.Contract,
		"El bootstrap aceptado es el del pipeline (commit "+shortCommit(setup.pipeline.Commit)+")",
		"El bootstrap del pipeline cambió desde que se aceptó la conexión. "+again)

	sameTarget := setup.account.SameTarget(conn.Outputs.Params)
	report.add(sameTarget,
		"La sesión de la nube apunta al destino de la conexión",
		"La sesión de la nube apunta a otro destino que la conexión: no se puede auditar lo que hay en la nube. "+
			"Cambia de cuenta o de suscripción y repite")
	if sameTarget {
		s.checkCloud(ctx, setup, environment, &report)
	}

	verification, err := s.admin.Verify(ctx, conn.ConnectionID)
	switch {
	case err != nil:
		report.add(false, "", "No se pudo probar el canje: "+forEnvironment(err, environment).Error())
	case verification.Conclusive():
		report.add(true, "El canje de un token de vex en la nube funciona", "")
	case verification.Status == VerificationActive:
		report.Items = append(report.Items, CheckItem{Status: CheckWarning,
			Text: "El canje no fue concluyente (la conexión sigue activa): " + verification.Message})
	default:
		report.add(false, "", "El canje de un token de vex no funciona: "+verification.Message)
	}
	return report, ctx.Err()
}

// checkCloud son los puntos que necesitan la sesión de la nube. Solo lee.
func (s *BootstrapService) checkCloud(ctx context.Context, setup *cloudSetup, environment string, report *CheckReport) {
	conn, projectID := setup.conn, setup.project.ID().String()
	trust := setup.adapter.Trust()

	subject, err := ConnectionSubject(projectID, environment, conn.ConnectionID)
	if err != nil {
		report.add(false, "", "No se puede calcular a quién representa la conexión: "+err.Error())
		return
	}
	want := Trust{Issuer: s.issuer, Subject: subject, Audience: trust.Audience()}
	findings, err := trust.Inspect(ctx, setup.account, *conn.Outputs, want)
	if err != nil {
		report.add(false, "", "No se pudo leer la confianza de la identidad de despliegue: "+err.Error())
	}
	for _, finding := range findings {
		report.add(finding.OK, finding.Text, finding.Text)
	}

	provider := setup.adapter.Bootstrap()
	region, err := provider.Region("", conn.Outputs)
	if err != nil {
		report.add(false, "", "No se puede saber dónde se aplicó el bootstrap: "+err.Error())
		return
	}
	// El nombre corto sale del nombre del proyecto en vexconfig.yaml, que es el que el portal conoce.
	slug := ProjectSlug(setup.project.Data().Name(), projectID)
	preview, err := provider.Preview(ctx, BootstrapRequest{
		Account: setup.account, Bootstrap: setup.bootstrap, Name: BootstrapOperationName(slug, environment),
		ProjectID: projectID, ProjectSlug: slug, Environment: environment, Region: region,
	})
	if err == nil {
		s.checkExposure(ctx, setup, preview, want, report)
	}
	switch {
	case err != nil:
		report.add(false, "", "No se pudo calcular la vista previa: "+err.Error())
	case preview.HasChanges():
		report.add(false, "", fmt.Sprintf("Lo que hay en la nube ya no es lo que deja el bootstrap (%s). "+
			"Ejecuta `vex bootstrap %s --reconnect` para ver los cambios y decidir", summarizeChanges(preview), environment))
	default:
		report.add(true, "Lo que hay en la nube coincide con el bootstrap: aplicarlo otra vez no cambiaría nada", "")
	}
	if unknown := preview.Unpreviewed(); err == nil && len(unknown) > 0 {
		report.Items = append(report.Items, CheckItem{Status: CheckWarning,
			Text: fmt.Sprintf("La nube no supo previsualizar %d recurso(s): sobre esos no hay comprobación", len(unknown))})
	}
}

// checkExposure comprueba que las identidades de la conexión son las que crea el bootstrap y que la de despliegue
// no tiene más permisos que los que el bootstrap declara.
func (s *BootstrapService) checkExposure(ctx context.Context, setup *cloudSetup, preview BootstrapPreview, want Trust, report *CheckReport) {
	exposed, err := setup.adapter.Trust().Exposure(ctx, setup.account, preview, *setup.conn.Outputs, want)
	var rejected *BootstrapRejectedError
	switch {
	case errors.As(err, &rejected):
		report.add(false, "", "La identidad de la conexión no es la que crea el bootstrap: "+rejected.Detail)
	case err != nil:
		report.add(false, "", "No se pudieron leer los permisos de la identidad de despliegue: "+err.Error())
	case len(exposed) > 0:
		lines := make([]string, len(exposed))
		for i, grant := range exposed {
			lines[i] = grant.Role + " → " + grant.Scope
		}
		report.add(false, "", fmt.Sprintf("La identidad de despliegue tiene %d permiso(s) que el bootstrap no declara: %s",
			len(exposed), strings.Join(lines, "; ")))
	default:
		report.add(true, "La identidad de despliegue solo tiene los permisos que declara el bootstrap", "")
	}
}

func summarizeChanges(preview BootstrapPreview) string {
	created, modified := 0, 0
	for _, change := range preview.Changes {
		switch change.Action {
		case ChangeCreate:
			created++
		case ChangeModify:
			modified++
		}
	}
	return fmt.Sprintf("%d por crear, %d por modificar", created, modified)
}

func shortCommit(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}
