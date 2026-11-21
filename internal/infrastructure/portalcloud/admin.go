package portalcloud

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/jairoprogramador/vex/internal/application"
	"github.com/jairoprogramador/vex/internal/infrastructure/portalclient"
)

var _ application.CloudConnectionAdmin = (*Portal)(nil)

func (p *Portal) Create(ctx context.Context, draft application.ConnectionDraft) (application.CreatedConnection, error) {
	resp, err := p.client.CreateCloudConnection(ctx, portalclient.CloudConnectionCreateRequest{
		ProjectID: draft.ProjectID, Environment: draft.Environment, ContractID: string(draft.Contract),
		BootstrapSHA256: draft.BootstrapSHA256, PipelineCommit: draft.PipelineCommit,
		Reconnect: draft.Reconnect, Target: draft.Target,
	})
	if err != nil {
		return application.CreatedConnection{}, classifyAdmin(err)
	}
	created := application.CreatedConnection{
		ConnectionID: resp.ConnectionID, Resumed: resp.Resumed, ReplacesConnectionID: resp.ReplacesConnectionID,
		Subject: resp.Subject, Issuer: resp.Issuer, Audience: resp.Audience, PendingExpiresAt: resp.PendingExpiresAt,
		Project: application.ConnectedProject{ID: resp.Project.ID, Name: resp.Project.Name, Slug: resp.Project.Slug},
		IsOwner: resp.IsOwner,
	}
	if resp.Owner != nil {
		created.Owner = application.ProjectOwner{Name: portalText(resp.Owner.Name), Email: portalText(resp.Owner.Email)}
	}
	return created, nil
}

func (p *Portal) Complete(ctx context.Context, completion application.ConnectionCompletion) (application.Verification, error) {
	resp, err := p.client.CompleteCloudConnection(ctx, portalclient.CloudConnectionCompleteRequest{
		ConnectionID: completion.ConnectionID, BootstrapSHA256: completion.BootstrapSHA256, Target: completion.Target,
		Outputs: portalclient.CloudConnectionOutputs{
			ContractID: string(completion.Outputs.ContractID),
			Params:     completion.Outputs.Params,
			Extras:     completion.Outputs.Extras,
		},
	})
	return verification(resp, err)
}

func (p *Portal) Verify(ctx context.Context, connectionID string) (application.Verification, error) {
	return verification(p.client.VerifyCloudConnection(ctx, connectionID))
}

func (p *Portal) Revoke(ctx context.Context, connectionID string) error {
	if err := p.client.RevokeCloudConnection(ctx, connectionID); err != nil {
		return classifyAdmin(err)
	}
	return nil
}

func verification(resp portalclient.CloudVerificationResponse, err error) (application.Verification, error) {
	if err != nil {
		return application.Verification{}, classifyAdmin(err)
	}
	return application.Verification{
		Status:     resp.Status,
		RetryAfter: time.Duration(resp.RetryAfterMS) * time.Millisecond,
		DetailCode: resp.DetailCode,
		Message:    portalText(resp.Message),
		Hint:       portalText(resp.Hint),
	}, nil
}

// portalText deja listo para mostrarse un texto libre del portal: vex no lo escribió, así que solo pasa ASCII
// imprimible y lo demás se ve escapado.
func portalText(text string) string { return application.ExternalText(text, 400) }

var (
	rejectionCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	// El campo rechazado es una ruta como outputs.params.tenant_id o outputs.extras.registryLoginServer.
	rejectionFieldPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.]{0,79}$`)
)

// classifyAdmin traduce los errores de las operaciones de administración de una conexión: la sesión y el límite
// se tratan como en classify; cualquier otra respuesta con código del portal (4xx, o 503 sin firma) es un rechazo
// que la aplicación sabe explicar. Del portal solo se conservan el código y el nombre del campo, y solo si tienen
// forma de identificador. Lo demás (red, 5xx) pasa tal cual y quien llama decide si reintenta.
func classifyAdmin(err error) error {
	var httpErr *portalclient.HTTPError
	switch {
	case errors.Is(err, portalclient.ErrUnauthorized), errors.Is(err, portalclient.ErrRateLimited):
		return classify(err)
	case !errors.As(err, &httpErr):
		return classify(err)
	case httpErr.StatusCode == 429:
		// Cualquier 429 de estas operaciones es un límite: se espera lo que diga Retry-After.
		return &throttledError{cause: err}
	case httpErr.StatusCode < 400 || (httpErr.StatusCode >= 500 && httpErr.StatusCode != 503):
		return err
	case !rejectionCodePattern.MatchString(httpErr.Code):
		return err
	}
	rejected := &application.ConnectionRejectedError{Code: httpErr.Code}
	if rejectionFieldPattern.MatchString(httpErr.Field) {
		rejected.Field = httpErr.Field
	}
	return rejected
}
