package portalclient

// CreateOrGetProjectRequest is the body of POST /functions/v1/create-or-get-project
// (§6.4 of plan_deploy.md). The CLI owns the canonical project identity: id,
// name, organization, team and repo coordinates flow from vexconfig.yaml.
//
// The portal MAY override pipeline / repo values when the project already
// exists with conflicting data. In that case the response includes an
// `authoritative` block and the CLI rewrites vexconfig.yaml locally
// (decision 11.2 of the plan).
type CreateOrGetProjectRequest struct {
	Project  ProjectPayload  `json:"project"`
	Pipeline PipelinePayload `json:"pipeline"`
}

// ProjectPayload mirrors the `project` field of §6.4. `Description` is
// optional and uses `omitempty` because the portal default differs from the
// CLI default and we want to let the server fill it when missing.
type ProjectPayload struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Team         string `json:"team"`
	Organization string `json:"organization"`
	Description  string `json:"description,omitempty"`
	RepoURL      string `json:"repo_url"`
	RepoRef      string `json:"repo_ref"`
}

// PipelinePayload mirrors the `pipeline` field of §6.4.
type PipelinePayload struct {
	URL          string `json:"url"`
	Ref          string `json:"ref"`
	RuntimeImage string `json:"runtime_image"`
	RuntimeTag   string `json:"runtime_tag"`
}

// CreateOrGetProjectResponse is the 200 body of the same endpoint.
//
// `Authoritative` is a pointer so its absence (the common path when the
// project was just created) round-trips as `null` in JSON.
type CreateOrGetProjectResponse struct {
	ProjectID     string             `json:"project_id"`
	PipelineID    string             `json:"pipeline_id"`
	Created       bool               `json:"created"`
	NeedsSync     bool               `json:"needs_sync"`
	Authoritative *AuthoritativeData `json:"authoritative,omitempty"`
}

// AuthoritativeData is the portal-canonical view of a project the CLI must
// adopt when the local vexconfig.yaml drifted from the server's record.
// The shape mirrors §6.4 1:1 and is used by RemoteExecutorService to
// rewrite the YAML in place.
type AuthoritativeData struct {
	Project  AuthoritativeProject  `json:"project"`
	Pipeline AuthoritativePipeline `json:"pipeline"`
}

// AuthoritativeProject is the project portion of AuthoritativeData.
type AuthoritativeProject struct {
	RepoURL string `json:"repo_url"`
	RepoRef string `json:"repo_ref"`
}

// AuthoritativePipeline is the pipeline portion of AuthoritativeData.
type AuthoritativePipeline struct {
	URL          string `json:"url"`
	Ref          string `json:"ref"`
	RuntimeImage string `json:"runtime_image"`
	RuntimeTag   string `json:"runtime_tag"`
}

// SyncPipelineRequest is the body of POST /functions/v1/sync-pipeline.
// The endpoint reads the GitHub layout of the pipeline repo and refreshes
// `steps`, `environments` and per-step `params` rows.
type SyncPipelineRequest struct {
	PipelineID string `json:"pipeline_id"`
}

// SyncPipelineResponse is intentionally tolerant: the portal returns a
// summary blob whose shape is not load-bearing for the CLI today. Decoding
// it into an `interface{}` lets us evolve the contract without breaking
// the client.
type SyncPipelineResponse struct {
	Status string `json:"status,omitempty"`
}

// TriggerDeployRequest is the body of POST /functions/v1/trigger-deploy
// (§6.5). Version is the project repo ref (branch or tag) being deployed;
// it is stored in executions.version for the portal audit trail.
// The CLI passes project.Data().Ref() from vexconfig.yaml; the portal
// passes the user-selected git ref.
type TriggerDeployRequest struct {
	PipelineID  string `json:"pipeline_id"`
	Environment string `json:"environment"`
	Step        string `json:"step"`
	Version     string `json:"version"`
}

// TriggerDeployResponse is the 200 body of the same endpoint.
type TriggerDeployResponse struct {
	ExecutionID string `json:"execution_id"`
	Status      string `json:"status"`
	FollowURL   string `json:"follow_url"`
	PortalURL   string `json:"portal_url"`
}

// CancelExecutionRequest is the body of POST /functions/v1/cancel-execution
// (§6.9). The portal handles idempotency: cancelling an already-finished
// execution is a no-op that returns 200 (or 404 if it never existed).
type CancelExecutionRequest struct {
	ExecutionID string `json:"execution_id"`
}

// CancelExecutionResponse is the 200 body of the same endpoint.
type CancelExecutionResponse struct {
	Status string `json:"status"`
}
