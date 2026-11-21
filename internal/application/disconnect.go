package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jairoprogramador/vex/internal/domain/project/aggregates"
)

// ErrNoCloudConnection: el ambiente no tiene una conexión que desconectar.
var ErrNoCloudConnection = errors.New("no hay conexión de nube que desconectar")

// DisconnectPlan es la conexión que se va a cortar.
type DisconnectPlan struct {
	Environment  string
	ConnectionID string
	Status       string
	Contract     ContractID

	project *aggregates.Project
	outputs *ConnectionOutputs
}

// DisconnectResult es hasta dónde llegó la desconexión. Revocar en el portal es lo único que vex garantiza;
// lo demás depende de la sesión de la nube y de sus bloqueos.
type DisconnectResult struct {
	Environment string
	// TrustRemoved: la confianza de vex ya no está en la nube.
	TrustRemoved bool
	// Pending explica por qué no se retiró la confianza; vacío si se retiró.
	Pending string
	// Manual son los comandos exactos para retirarla a mano.
	Manual []string
	// Cleanup son los comandos para borrar lo que creó el bootstrap. vex no los ejecuta.
	Cleanup []string
}

// DisconnectService corta la conexión de un ambiente: primero en el portal, que deja de emitir tokens, y después
// retira la confianza de vex de la nube. No borra recursos: son de quien aplicó el bootstrap.
type DisconnectService struct {
	workspace *Workspace
	preparer  *CloudPreparer
	adapters  CloudAdapters
	admin     CloudConnectionAdmin
	journal   BootstrapJournal
	notify    Notifier
	issuer    string
	now       func() time.Time
}

func NewDisconnectService(
	workspace *Workspace, preparer *CloudPreparer, adapters CloudAdapters, admin CloudConnectionAdmin,
	journal BootstrapJournal, notify Notifier, issuer string,
) *DisconnectService {
	return &DisconnectService{
		workspace: workspace, preparer: preparer, adapters: adapters, admin: admin,
		journal: journal, notify: notify, issuer: issuer, now: time.Now,
	}
}

// Plan busca la conexión del ambiente. Sin una que cortar devuelve ErrNoCloudConnection.
func (s *DisconnectService) Plan(ctx context.Context, environment string) (*DisconnectPlan, error) {
	project, preparation, err := portalProject(ctx, s.workspace, s.preparer, environment)
	if err != nil {
		return nil, err
	}
	defer preparation.Close()
	conn, err := preparation.connection(ctx, environment)
	if err != nil {
		return nil, err
	}
	if conn.ConnectionID == "" || conn.Status == "absent" || conn.Status == "revoked" {
		return nil, fmt.Errorf("%w en '%s'", ErrNoCloudConnection, environment)
	}
	plan := &DisconnectPlan{
		Environment: environment, ConnectionID: conn.ConnectionID, Status: conn.Status,
		project: project, outputs: conn.Outputs,
	}
	if conn.Outputs != nil {
		plan.Contract = conn.Outputs.ContractID
	}
	return plan, nil
}

// Run corta la conexión. Si el portal no confirma la revocación devuelve el error y no toca la nube: mientras
// el portal pueda emitir tokens, la confianza no se da por retirada.
func (s *DisconnectService) Run(ctx context.Context, plan *DisconnectPlan) (DisconnectResult, error) {
	result := DisconnectResult{Environment: plan.Environment}
	if err := s.admin.Revoke(ctx, plan.ConnectionID); err != nil {
		return result, fmt.Errorf("el portal no confirmó la revocación; no se tocó nada en la nube: %w",
			forEnvironment(err, plan.Environment))
	}
	s.notify.Info("Conexión revocada en el portal: vex ya no emite tokens para '" + plan.Environment + "'.")

	s.removeTrust(ctx, plan, &result)
	outcome := "trust_removed"
	if !result.TrustRemoved {
		outcome = "revoked_only"
	}
	record := AcceptanceRecord{
		At: s.now().UTC(), Action: "disconnect", Outcome: outcome, ProjectID: plan.project.ID().String(),
		Environment: plan.Environment, Contract: string(plan.Contract), ConnectionID: plan.ConnectionID,
	}
	if err := s.journal.Record(record); err != nil {
		s.notify.Warn("No se pudo guardar la constancia local de la desconexión: " + err.Error())
	}
	return result, nil
}

// removeTrust retira la confianza de la nube si la sesión es la del destino de la conexión. Lo que no pueda hacer
// queda explicado en result, con los comandos para terminarlo a mano.
func (s *DisconnectService) removeTrust(ctx context.Context, plan *DisconnectPlan, result *DisconnectResult) {
	if plan.outputs == nil {
		result.Pending = "la conexión no llegó a activarse, así que el portal no sabe qué dejó en la nube. " +
			"Si el bootstrap llegó a aplicarse, borra a mano la credencial federada de la identidad de despliegue"
		return
	}
	adapter, err := s.adapters.Lookup(plan.outputs.ContractID)
	if err != nil {
		result.Pending = err.Error()
		return
	}
	projectID := plan.project.ID().String()
	guide := adapter.Trust().Cleanup(*plan.outputs,
		BootstrapOperationName(ProjectSlug(plan.project.Data().Name(), projectID), plan.Environment))
	result.Cleanup = guide.DeleteResources
	result.Manual = guide.RemoveTrust

	subject, err := ConnectionSubject(projectID, plan.Environment, plan.ConnectionID)
	if err != nil {
		result.Pending = err.Error()
		return
	}
	account, err := adapter.Bootstrap().Account(ctx)
	if err != nil {
		result.Pending = "no hay una sesión de la nube con la que retirar la confianza (" + err.Error() + ")"
		return
	}
	if !account.SameTarget(plan.outputs.Params) {
		result.Pending = "la sesión de la nube apunta a otro destino que la conexión; no se toca nada ahí"
		return
	}
	want := Trust{Issuer: s.issuer, Subject: subject, Audience: adapter.Trust().Audience()}
	var locked *TrustLockedError
	switch err := adapter.Trust().Remove(ctx, account, *plan.outputs, want); {
	case err == nil:
		result.TrustRemoved = true
		result.Manual = nil
	case errors.As(err, &locked):
		result.Pending = "un bloqueo de la nube protege la identidad de despliegue y vex no lo quita por su cuenta"
		result.Manual = locked.Commands
	default:
		result.Pending = "la nube no dejó retirar la confianza: " + err.Error()
	}
}
