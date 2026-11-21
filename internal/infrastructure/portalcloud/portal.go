// Package portalcloud adapta el cliente HTTP del portal a los puertos de nube de la aplicación
// (application.CloudConnections y application.TokenSource): traduce las respuestas y clasifica los errores en
// definitivos y transitorios. No conoce ninguna nube: los datos de la conexión pasan sin interpretarse.
package portalcloud

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jairoprogramador/vex/internal/application"
	"github.com/jairoprogramador/vex/internal/infrastructure/portalauth"
	"github.com/jairoprogramador/vex/internal/infrastructure/portalclient"
)

// Portal es el portal visto como fuente de conexiones y de tokens de nube.
type Portal struct {
	client *portalclient.PortalClient
}

func New(client *portalclient.PortalClient) *Portal {
	return &Portal{client: client}
}

var (
	_ application.CloudConnections = (*Portal)(nil)
	_ application.TokenSource      = (*Portal)(nil)
)

func (p *Portal) CloudConnection(ctx context.Context, projectID, environment string) (application.CloudConnectionInfo, error) {
	resp, err := p.client.CloudConnection(ctx, projectID, environment)
	if err != nil {
		return application.CloudConnectionInfo{}, classify(err)
	}
	info := application.CloudConnectionInfo{
		Status:          resp.Status,
		ConnectionID:    resp.ConnectionID,
		BootstrapSHA256: resp.BootstrapSHA256,
		PipelineCommit:  resp.PipelineCommit,
		Reason:          portalText(resp.Reason),
		Hint:            portalText(resp.Hint),
	}
	if resp.Outputs != nil {
		info.Outputs = &application.ConnectionOutputs{
			ContractID: application.ContractID(resp.Outputs.ContractID),
			Params:     resp.Outputs.Params,
			Extras:     resp.Outputs.Extras,
		}
	}
	return info, nil
}

func (p *Portal) Issue(ctx context.Context, req application.TokenRequest) (application.IssuedToken, error) {
	resp, err := p.client.CloudToken(ctx, portalclient.CloudTokenRequest{
		ProjectID:   req.ProjectID,
		Environment: req.Environment,
		AttemptID:   req.AttemptID,
		Commit:      req.Commit,
		ExecutionID: req.ExecutionID,
	})
	if err != nil {
		return application.IssuedToken{}, classify(err)
	}
	return application.IssuedToken{Value: resp.Token, ExpiresAt: resp.ExpiresAt, RefreshAfter: resp.RefreshAfter}, nil
}

// classify marca los errores del portal que la aplicación trata distinto, sin perder el original: sin sesión,
// sin permiso o sin conexión activa es definitivo (y dice cuál de los tres, porque cada uno se arregla de una
// forma); el límite de emisiones es transitorio y dice cuánto esperar. El resto (firma o auditoría no
// disponibles, red) pasa tal cual y quien llama lo trata como transitorio.
func classify(err error) error {
	switch {
	case errors.Is(err, portalclient.ErrUnauthorized), errors.Is(err, portalauth.ErrTokenNotFound):
		return fmt.Errorf("%w: %w: %w", application.ErrCloudTokenDenied, application.ErrPortalSessionInvalid, err)
	case errors.Is(err, portalclient.ErrConnectionNotActive):
		return fmt.Errorf("%w: %w: %w", application.ErrCloudTokenDenied, application.ErrCloudConnectionInactive, err)
	case errors.Is(err, portalclient.ErrForbidden):
		return fmt.Errorf("%w: %w", application.ErrCloudTokenDenied, err)
	case errors.Is(err, portalclient.ErrRateLimited):
		return &throttledError{cause: err}
	default:
		return err
	}
}

// throttledError es el límite de emisiones del portal visto como application.ErrCloudTokenThrottled.
type throttledError struct {
	cause error
}

var _ application.RetryAfter = (*throttledError)(nil)

func (e *throttledError) Error() string {
	return fmt.Sprintf("%v: %v", application.ErrCloudTokenThrottled, e.cause)
}

func (e *throttledError) Unwrap() []error {
	return []error{application.ErrCloudTokenThrottled, e.cause}
}

// RetryAfter es lo que el portal pidió esperar; cero si no lo dijo.
func (e *throttledError) RetryAfter() time.Duration {
	var httpErr *portalclient.HTTPError
	if errors.As(e.cause, &httpErr) {
		return time.Duration(httpErr.RetryAfter) * time.Second
	}
	return 0
}
