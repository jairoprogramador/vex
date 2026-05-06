package portalclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/jairoprogramador/vex/internal/infrastructure/portalauth"
)

// PortalClient is a thin REST client for the portal edge functions
// involved in the deploy lifecycle. It is concurrency-safe as long as the
// underlying *http.Client and *FileTokenStore are.
//
// The client reads the bearer token lazily on every call so the caller
// can swap credentials (e.g. after a re-login) without rebuilding the
// struct.
type PortalClient struct {
	baseURL    string
	tokenStore *portalauth.FileTokenStore
	httpClient *http.Client
}

// NewPortalClient wires a client targeted at baseURL. baseURL must NOT
// include the `/functions/v1` prefix — the client appends it per call to
// keep the request sites greppable.
//
// Passing a nil httpClient yields http.DefaultClient. Tests pass an
// httptest.NewServer-backed client.
func NewPortalClient(baseURL string, tokenStore *portalauth.FileTokenStore, httpClient *http.Client) *PortalClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &PortalClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		tokenStore: tokenStore,
		httpClient: httpClient,
	}
}

// CreateOrGetProject calls the create-or-get-project edge function. See
// portalclient.CreateOrGetProjectRequest for the contract.
func (c *PortalClient) CreateOrGetProject(ctx context.Context, req CreateOrGetProjectRequest) (CreateOrGetProjectResponse, error) {
	var resp CreateOrGetProjectResponse
	if err := c.do(ctx, "create-or-get-project", req, &resp); err != nil {
		return CreateOrGetProjectResponse{}, err
	}
	return resp, nil
}

// SyncPipeline calls the sync-pipeline edge function with the given
// pipeline ID. The CLI invokes this only when CreateOrGetProject set
// `needs_sync = true`.
func (c *PortalClient) SyncPipeline(ctx context.Context, pipelineID string) error {
	if pipelineID == "" {
		return errors.New("portalclient: sync pipeline: pipeline_id is required")
	}
	var resp SyncPipelineResponse
	return c.do(ctx, "sync-pipeline", SyncPipelineRequest{PipelineID: pipelineID}, &resp)
}

// TriggerDeploy calls the trigger-deploy edge function and returns the
// execution metadata the user needs to follow the run.
func (c *PortalClient) TriggerDeploy(ctx context.Context, req TriggerDeployRequest) (TriggerDeployResponse, error) {
	var resp TriggerDeployResponse
	if err := c.do(ctx, "trigger-deploy", req, &resp); err != nil {
		return TriggerDeployResponse{}, err
	}
	return resp, nil
}

// CancelExecution requests cancellation of an in-flight execution. The
// portal is responsible for destroying the Fly Machine asynchronously.
func (c *PortalClient) CancelExecution(ctx context.Context, executionID string) error {
	if executionID == "" {
		return errors.New("portalclient: cancel execution: execution_id is required")
	}
	var resp CancelExecutionResponse
	return c.do(ctx, "cancel-execution", CancelExecutionRequest{ExecutionID: executionID}, &resp)
}

// do is the shared transport: marshal body → bearer-authenticated POST →
// classify status code → decode JSON. Any non-success status is mapped to
// a sentinel error wrapping a *HTTPError.
func (c *PortalClient) do(ctx context.Context, endpoint string, body, out any) error {
	if c.tokenStore == nil {
		return errors.New("portalclient: token store is not configured")
	}
	token, err := c.tokenStore.Load()
	if err != nil {
		// Bubble portalauth.ErrTokenNotFound as-is so the caller can
		// trigger the device-code flow without unwrapping.
		return err
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("portalclient: marshal %s: %w", endpoint, err)
	}

	url := c.baseURL + "/functions/v1/" + endpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("portalclient: build %s request: %w", endpoint, err)
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("portalclient: call %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out == nil {
			return nil
		}
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			// An empty body on a 2xx is not an error: nothing to decode.
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("portalclient: decode %s response: %w", endpoint, err)
		}
		return nil
	}

	return classifyError(endpoint, resp)
}

// classifyError reads the response body, parses any structured error code,
// and returns the matching sentinel wrapping a *HTTPError.
func classifyError(endpoint string, resp *http.Response) error {
	httpErr := readHTTPError(resp)

	// Special-case structured error codes that override the status code
	// branches (e.g. 429 with concurrent_execution_limit).
	switch httpErr.Code {
	case "concurrent_execution_limit":
		return joinErr(ErrUserConcurrencyLimit, httpErr, endpoint)
	case "global_capacity_reached":
		return joinErr(ErrGlobalCapacityReached, httpErr, endpoint)
	}

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return joinErr(ErrUnauthorized, httpErr, endpoint)
	case http.StatusForbidden:
		return joinErr(ErrForbidden, httpErr, endpoint)
	case http.StatusNotFound:
		return joinErr(ErrNotFound, httpErr, endpoint)
	case http.StatusTooManyRequests:
		return joinErr(ErrUserConcurrencyLimit, httpErr, endpoint)
	case http.StatusBadGateway:
		return joinErr(ErrFlyAPIFailure, httpErr, endpoint)
	case http.StatusServiceUnavailable:
		return joinErr(ErrGlobalCapacityReached, httpErr, endpoint)
	default:
		return fmt.Errorf("portalclient: %s: %w", endpoint, httpErr)
	}
}

// readHTTPError decodes the JSON envelope `{error, message, error_description}`
// and falls back to the trimmed raw body when the response is not JSON.
// Retry-After is parsed best-effort.
func readHTTPError(resp *http.Response) *HTTPError {
	httpErr := &HTTPError{StatusCode: resp.StatusCode}
	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			httpErr.RetryAfter = secs
		}
	}

	body, _ := io.ReadAll(resp.Body)
	if len(body) == 0 {
		return httpErr
	}

	var payload struct {
		Error            string `json:"error"`
		Message          string `json:"message"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		httpErr.Code = payload.Error
		httpErr.Message = payload.Message
		if httpErr.Message == "" {
			httpErr.Message = payload.ErrorDescription
		}
	}
	if httpErr.Code == "" && httpErr.Message == "" {
		httpErr.Message = strings.TrimSpace(string(body))
	}
	return httpErr
}

// joinErr returns an error chain where errors.Is yields the sentinel and
// errors.As yields the *HTTPError. The endpoint name is prepended for log
// readability.
func joinErr(sentinel error, httpErr *HTTPError, endpoint string) error {
	return fmt.Errorf("portalclient: %s: %w", endpoint, &joinedError{sentinel: sentinel, http: httpErr})
}

// joinedError carries both the sentinel and the structured *HTTPError so
// callers can errors.Is(...) for the high-level branch and errors.As(...,
// &httpErr) for the wire details.
type joinedError struct {
	sentinel error
	http     *HTTPError
}

func (e *joinedError) Error() string {
	return e.http.Error()
}

func (e *joinedError) Is(target error) bool {
	return errors.Is(e.sentinel, target)
}

func (e *joinedError) Unwrap() []error {
	return []error{e.sentinel, e.http}
}
