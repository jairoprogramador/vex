package portalclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// cloudConnectionActive is the only status that carries outputs.
const cloudConnectionActive = "active"

// CloudConnectionResponse mirrors the cli-cloud-connection body the CLI
// reads. JSON nulls decode to the zero value of each field.
type CloudConnectionResponse struct {
	Status       string `json:"status"`
	ConnectionID string `json:"connection_id"`
	Reason       string `json:"reason"`
	Hint         string `json:"hint"`
	// BootstrapSHA256 and PipelineCommit identify the pipeline bootstrap the
	// connection was created from.
	BootstrapSHA256 string `json:"bootstrap_sha256"`
	PipelineCommit  string `json:"pipeline_commit"`
	// Outputs is only present when Status is "active".
	Outputs *CloudConnectionOutputs `json:"outputs"`
}

// CloudConnectionOutputs is what the bootstrap left in the user's cloud.
// Params and Extras are opaque to this client: only the contract adapter
// reads them.
type CloudConnectionOutputs struct {
	ContractID string            `json:"contract_id"`
	Params     map[string]string `json:"params"`
	Extras     map[string]string `json:"extras"`
}

// CloudTokenRequest mirrors the cli-cloud-token body. Empty optional fields
// are omitted: the endpoint rejects unknown or malformed keys.
type CloudTokenRequest struct {
	ProjectID   string `json:"project_id"`
	Environment string `json:"environment"`
	AttemptID   string `json:"attempt_id,omitempty"`
	Commit      string `json:"commit,omitempty"`
	ExecutionID string `json:"execution_id,omitempty"`
}

// CloudTokenResponse mirrors the cli-cloud-token success body.
type CloudTokenResponse struct {
	Token        string    `json:"token"`
	ExpiresAt    time.Time `json:"expires_at"`
	RefreshAfter time.Time `json:"refresh_after"`
}

// String hides Token so the JWT never reaches a log or an error message.
func (r CloudTokenResponse) String() string {
	return fmt.Sprintf("CloudTokenResponse{Token: [redacted], ExpiresAt: %s, RefreshAfter: %s}",
		r.ExpiresAt.Format(time.RFC3339), r.RefreshAfter.Format(time.RFC3339))
}

// GoString covers %#v, which bypasses String.
func (r CloudTokenResponse) GoString() string { return r.String() }

// CloudConnection calls the cli-cloud-connection edge function (GET) and
// returns the state of the environment's cloud connection. A missing
// connection is not an error: it comes back with Status "absent".
func (c *PortalClient) CloudConnection(ctx context.Context, projectID, environment string) (CloudConnectionResponse, error) {
	if projectID == "" || environment == "" {
		return CloudConnectionResponse{}, errors.New("portalclient: cloud connection: project_id and environment are required")
	}
	query := url.Values{"project_id": {projectID}, "environment": {environment}}

	var resp CloudConnectionResponse
	if err := c.send(ctx, http.MethodGet, "cli-cloud-connection", query, nil, &resp); err != nil {
		return CloudConnectionResponse{}, err
	}
	if resp.Status == "" {
		return CloudConnectionResponse{}, errors.New("portalclient: cli-cloud-connection devolvió una respuesta sin estado")
	}
	// Never hand out a half-described active connection.
	if resp.Status == cloudConnectionActive && (resp.Outputs == nil || resp.Outputs.ContractID == "") {
		return CloudConnectionResponse{}, errors.New("portalclient: cli-cloud-connection devolvió una conexión activa sin outputs")
	}
	return resp, nil
}

// CloudToken calls the cli-cloud-token edge function and returns a
// short-lived JWT for the environment's active cloud connection. It never
// retries: every call counts against the issuance rate limit.
func (c *PortalClient) CloudToken(ctx context.Context, req CloudTokenRequest) (CloudTokenResponse, error) {
	if req.ProjectID == "" || req.Environment == "" {
		return CloudTokenResponse{}, errors.New("portalclient: cloud token: project_id and environment are required")
	}

	var resp CloudTokenResponse
	if err := c.do(ctx, "cli-cloud-token", req, &resp); err != nil {
		return CloudTokenResponse{}, err
	}
	// refresh_after drives the renewal loop: without it the caller would
	// spin, so its absence is as invalid as a missing or expired token.
	if resp.Token == "" || !resp.ExpiresAt.After(c.now()) || resp.RefreshAfter.IsZero() {
		return CloudTokenResponse{}, errors.New("portalclient: cli-cloud-token devolvió un token inválido")
	}
	return resp, nil
}
