package portalclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"
)

// targetKeyPattern is the shape of a contract-specific target field name.
var targetKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

// CloudConnectionCreateRequest mirrors the cli-cloud-connection-create body.
// Target holds the contract-specific fields (tenant_id and subscription_id
// for azure/v1); they travel at the top level of the JSON object.
type CloudConnectionCreateRequest struct {
	ProjectID       string
	Environment     string
	ContractID      string
	BootstrapSHA256 string
	PipelineCommit  string
	Reconnect       bool
	Target          map[string]string
}

func (r CloudConnectionCreateRequest) MarshalJSON() ([]byte, error) {
	return marshalWithTarget(map[string]any{
		"project_id":       r.ProjectID,
		"environment":      r.Environment,
		"contract_id":      r.ContractID,
		"bootstrap_sha256": r.BootstrapSHA256,
		"pipeline_commit":  r.PipelineCommit,
		"reconnect":        r.Reconnect,
	}, r.Target)
}

// CloudConnectionCreateResponse mirrors the create success body.
type CloudConnectionCreateResponse struct {
	ConnectionID         string    `json:"connection_id"`
	Resumed              bool      `json:"resumed"`
	ReplacesConnectionID string    `json:"replaces_connection_id"`
	Subject              string    `json:"subject"`
	Issuer               string    `json:"issuer"`
	Audience             string    `json:"audience"`
	PendingExpiresAt     time.Time `json:"pending_expires_at"`
	Project              struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"project"`
	// Owner is null when the project has no owner or admin on record.
	Owner *struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"owner"`
	IsOwner bool `json:"is_owner"`
}

// CloudConnectionCompleteRequest mirrors the cli-cloud-connection-complete
// body. Target repeats the fields declared on create.
type CloudConnectionCompleteRequest struct {
	ConnectionID    string
	BootstrapSHA256 string
	Outputs         CloudConnectionOutputs
	Target          map[string]string
}

func (r CloudConnectionCompleteRequest) MarshalJSON() ([]byte, error) {
	outputs := map[string]any{"contract_id": r.Outputs.ContractID, "params": r.Outputs.Params}
	if len(r.Outputs.Extras) > 0 {
		outputs["extras"] = r.Outputs.Extras
	}
	return marshalWithTarget(map[string]any{
		"connection_id":    r.ConnectionID,
		"bootstrap_sha256": r.BootstrapSHA256,
		"outputs":          outputs,
	}, r.Target)
}

// marshalWithTarget adds the target fields to the body. A target key that
// is malformed or shadows a fixed field is a programming error upstream and
// is refused rather than sent.
func marshalWithTarget(body map[string]any, target map[string]string) ([]byte, error) {
	for key, value := range target {
		if _, fixed := body[key]; fixed || !targetKeyPattern.MatchString(key) {
			return nil, fmt.Errorf("portalclient: invalid target field %q", key)
		}
		body[key] = value
	}
	return json.Marshal(body)
}

// CloudVerificationResponse mirrors the body of complete and verify: the
// outcome of exchanging a real token in the user's cloud.
type CloudVerificationResponse struct {
	Status       string `json:"status"`
	RetryAfterMS int    `json:"retry_after_ms"`
	DetailCode   string `json:"detail_code"`
	Message      string `json:"message"`
	Hint         string `json:"hint"`
}

// CloudConnectionRevokeRequest mirrors the cli-cloud-connection-revoke body.
type CloudConnectionRevokeRequest struct {
	ConnectionID string `json:"connection_id"`
	Reason       string `json:"reason"`
}

// CreateCloudConnection opens (or resumes) the pending connection of an
// environment and returns what the portal fixed for it.
func (c *PortalClient) CreateCloudConnection(ctx context.Context, req CloudConnectionCreateRequest) (CloudConnectionCreateResponse, error) {
	var resp CloudConnectionCreateResponse
	if err := c.do(ctx, "cli-cloud-connection-create", req, &resp); err != nil {
		return CloudConnectionCreateResponse{}, err
	}
	if resp.ConnectionID == "" || resp.Subject == "" || resp.Issuer == "" || resp.Audience == "" {
		return CloudConnectionCreateResponse{}, errors.New("portalclient: cli-cloud-connection-create devolvió una conexión incompleta")
	}
	return resp, nil
}

// CompleteCloudConnection hands the bootstrap outputs to the portal, which
// verifies the connection. It is idempotent.
func (c *PortalClient) CompleteCloudConnection(ctx context.Context, req CloudConnectionCompleteRequest) (CloudVerificationResponse, error) {
	return c.verification(ctx, "cli-cloud-connection-complete", req)
}

// VerifyCloudConnection asks the portal to verify the connection again.
func (c *PortalClient) VerifyCloudConnection(ctx context.Context, connectionID string) (CloudVerificationResponse, error) {
	return c.verification(ctx, "cli-cloud-connection-verify", map[string]string{"connection_id": connectionID})
}

func (c *PortalClient) verification(ctx context.Context, endpoint string, body any) (CloudVerificationResponse, error) {
	var resp CloudVerificationResponse
	if err := c.do(ctx, endpoint, body, &resp); err != nil {
		return CloudVerificationResponse{}, err
	}
	switch resp.Status {
	case "active", "verifying", "failed":
		return resp, nil
	}
	return CloudVerificationResponse{}, fmt.Errorf("portalclient: %s devolvió un estado desconocido", endpoint)
}

// RevokeCloudConnection cuts token issuance for the connection. It fails
// closed: anything but an explicit "revoked" from the portal is an error.
func (c *PortalClient) RevokeCloudConnection(ctx context.Context, connectionID string) error {
	var resp struct {
		Status string `json:"status"`
	}
	req := CloudConnectionRevokeRequest{ConnectionID: connectionID, Reason: "user"}
	if err := c.do(ctx, "cli-cloud-connection-revoke", req, &resp); err != nil {
		return err
	}
	if resp.Status != "revoked" {
		return errors.New("portalclient: cli-cloud-connection-revoke no confirmó la revocación")
	}
	return nil
}
