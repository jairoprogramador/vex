// Package portalclient implements the HTTP client the CLI uses against the
// Vex portal edge functions (`cli-create-or-get-project`, `trigger-deploy`,
// `cancel-execution`, `sync-pipeline`). It is intentionally a thin wrapper
// over the JSON REST surface the portal exposes; the higher-level
// orchestration lives in `internal/application/remote_executor.go`.
package portalclient

import "errors"

// Sentinel errors returned by every method on PortalClient. Callers branch
// on them via errors.Is. A *HTTPError is wrapped underneath so the original
// status code and parsed error payload remain available via errors.As.
var (
	// ErrUnauthorized signals a 401 from any portal endpoint. The token is
	// missing, malformed, expired or revoked. The CLI treats this as a
	// trigger to re-run `vex auth login`.
	ErrUnauthorized = errors.New("portalclient: unauthorized")

	// ErrForbidden signals a 403, typically because the user is not a
	// member of the project they are trying to operate on.
	ErrForbidden = errors.New("portalclient: forbidden")

	// ErrNotFound signals a 404. For `cancel-execution` it means the
	// execution does not exist or belongs to another tenant; for
	// `sync-pipeline` it means the pipeline row vanished.
	ErrNotFound = errors.New("portalclient: not found")

	// ErrUserConcurrencyLimit maps to HTTP 429 with body
	// `{"error": "concurrent_execution_limit"}`. The user has reached the
	// per-tenant cap (3 concurrent executions in the MVP).
	ErrUserConcurrencyLimit = errors.New("portalclient: user concurrency limit reached")

	// ErrGlobalCapacityReached maps to HTTP 503 with body
	// `{"error": "global_capacity_reached"}`. The portal recommends a
	// Retry-After header; callers can read it via *HTTPError.RetryAfter.
	ErrGlobalCapacityReached = errors.New("portalclient: global capacity reached")

	// ErrFlyAPIFailure maps to HTTP 502. The portal could not dispatch the
	// Machine to Fly. Surfaced as-is to the user; retrying may help.
	ErrFlyAPIFailure = errors.New("portalclient: fly api failure")

	// ErrConflict maps to HTTP 409. For `cancel-execution` it means the
	// execution is already in a terminal state (succeeded/failed/canceled)
	// and cannot transition any further.
	ErrConflict = errors.New("portalclient: conflict")
)

// HTTPError captures the raw shape of a non-success response. It is wrapped
// underneath the sentinel error so callers that need finer-grained context
// (e.g. the Retry-After hint on 503) can recover it via errors.As.
type HTTPError struct {
	StatusCode int
	// Code is the value of the JSON `error` field when the response body
	// was a structured error (e.g. "concurrent_execution_limit").
	Code string
	// Message is the JSON `message` or `error_description` field, when
	// present. Falls back to the trimmed raw body for non-JSON payloads.
	Message string
	// RetryAfter is the parsed `Retry-After` header in seconds, or 0 when
	// absent or unparseable. Only meaningful for 503/429 responses.
	RetryAfter int
}

// Error implements the error interface with a stable shape suitable for
// CLI display. It avoids leaking the raw body to keep terminal output
// readable.
func (e *HTTPError) Error() string {
	if e.Code != "" {
		return e.codeMessage()
	}
	if e.Message != "" {
		return e.fallbackMessage()
	}
	return httpStatusMessage(e.StatusCode)
}

func (e *HTTPError) codeMessage() string {
	if e.Message == "" {
		return e.Code
	}
	return e.Code + ": " + e.Message
}

func (e *HTTPError) fallbackMessage() string {
	return httpStatusMessage(e.StatusCode) + ": " + e.Message
}

func httpStatusMessage(status int) string {
	switch status {
	case 401:
		return "unauthorized"
	case 403:
		return "forbidden"
	case 404:
		return "not found"
	case 409:
		return "conflict"
	case 429:
		return "too many requests"
	case 502:
		return "bad gateway"
	case 503:
		return "service unavailable"
	default:
		return "portal error"
	}
}
