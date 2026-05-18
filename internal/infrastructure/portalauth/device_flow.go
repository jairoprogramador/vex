package portalauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// httpDoer abstracts *http.Client so tests can inject lightweight stubs.
// It mirrors the standard signature of http.Client.Do.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// DeviceFlowClient talks to the portal's `device-code` and `device-token`
// edge functions. It is safe for concurrent use by multiple goroutines as
// long as the underlying *http.Client is.
type DeviceFlowClient struct {
	portalURL    string
	httpClient   httpDoer
	slowDownStep time.Duration // bump applied to interval on `slow_down`; defaults to 5s
	anonKey      string        // Supabase anon JWT; sent as Authorization header
}

// defaultSlowDownStep is the increment applied to the polling interval when
// the server replies with `slow_down`, as required by RFC 8628 §3.5.
const defaultSlowDownStep = 5 * time.Second

// NewDeviceFlowClient returns a client targeted at portalURL. A 30s timeout
// is applied to every individual request; the polling loop respects ctx and
// the server-driven `expired_token` for total deadline.
func NewDeviceFlowClient(portalURL, anonKey string) *DeviceFlowClient {
	return &DeviceFlowClient{
		portalURL: strings.TrimRight(portalURL, "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		slowDownStep: defaultSlowDownStep,
		anonKey:      anonKey,
	}
}

// Start initiates the device authorization flow (§6.1). On success it
// returns the device/user codes and the verification URLs the caller must
// surface to the end user.
func (c *DeviceFlowClient) Start(ctx context.Context) (DeviceCodeResponse, error) {
	body, err := json.Marshal(map[string]string{"client_id": ClientID})
	if err != nil {
		return DeviceCodeResponse{}, fmt.Errorf("marshal device-code request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.portalURL+"/functions/v1/device-code", bytes.NewReader(body))
	if err != nil {
		return DeviceCodeResponse{}, fmt.Errorf("build device-code request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.anonKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.anonKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return DeviceCodeResponse{}, fmt.Errorf("call device-code: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return DeviceCodeResponse{}, decodeError(resp, "device-code")
	}

	var out DeviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return DeviceCodeResponse{}, fmt.Errorf("decode device-code response: %w", err)
	}
	if out.DeviceCode == "" || out.UserCode == "" {
		return DeviceCodeResponse{}, fmt.Errorf("device-code: malformed response (missing codes)")
	}
	return out, nil
}

// Poll repeatedly hits the `device-token` endpoint (§6.2) until the user
// approves the device, the server expires the code, or ctx is canceled.
//
// The interval is enforced by the client; if the server replies with
// `slow_down` the interval is increased by 5s as required by the spec.
// Network errors are treated as transient and retried respecting the same
// cadence.
func (c *DeviceFlowClient) Poll(ctx context.Context, deviceCode string, interval time.Duration) (TokenResponse, error) {
	if interval <= 0 {
		interval = 5 * time.Second
	}

	for {
		select {
		case <-ctx.Done():
			return TokenResponse{}, ctx.Err()
		case <-time.After(interval):
		}

		token, status, err := c.exchangeToken(ctx, deviceCode)
		switch status {
		case tokenStatusSuccess:
			return token, nil
		case tokenStatusPending:
			continue
		case tokenStatusSlowDown:
			step := c.slowDownStep
			if step <= 0 {
				step = defaultSlowDownStep
			}
			interval += step
			continue
		case tokenStatusExpired:
			return TokenResponse{}, ErrDeviceCodeExpired
		case tokenStatusDenied:
			return TokenResponse{}, ErrAccessDenied
		}
		// transient transport/decoding error: retry on the next tick unless
		// the context is already gone.
		if ctx.Err() != nil {
			return TokenResponse{}, ctx.Err()
		}
		if err != nil {
			// keep looping on transient errors; the server-driven
			// expired_token will eventually break us out of the loop.
			continue
		}
	}
}

// tokenStatus is the result classification of a single device-token call.
type tokenStatus int

const (
	tokenStatusUnknown tokenStatus = iota
	tokenStatusSuccess
	tokenStatusPending
	tokenStatusSlowDown
	tokenStatusExpired
	tokenStatusDenied
)

// exchangeToken performs a single device-token request and returns either
// the successful TokenResponse or the classified status of an intermediate
// response. Network/decoding errors yield tokenStatusUnknown plus the
// underlying error so the caller can decide whether to retry.
func (c *DeviceFlowClient) exchangeToken(ctx context.Context, deviceCode string) (TokenResponse, tokenStatus, error) {
	body, err := json.Marshal(map[string]string{
		"grant_type":  DeviceCodeGrantType,
		"device_code": deviceCode,
		"client_id":   ClientID,
	})
	if err != nil {
		return TokenResponse{}, tokenStatusUnknown, fmt.Errorf("marshal device-token request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.portalURL+"/functions/v1/device-token", bytes.NewReader(body))
	if err != nil {
		return TokenResponse{}, tokenStatusUnknown, fmt.Errorf("build device-token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.anonKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.anonKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return TokenResponse{}, tokenStatusUnknown, fmt.Errorf("call device-token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var out TokenResponse
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return TokenResponse{}, tokenStatusUnknown, fmt.Errorf("decode device-token response: %w", err)
		}
		if out.AccessToken == "" {
			return TokenResponse{}, tokenStatusUnknown, fmt.Errorf("device-token: empty access_token in success response")
		}
		return out, tokenStatusSuccess, nil
	}

	// 4xx responses with a known error code are intermediate states.
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		var payload struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&payload)
		switch payload.Error {
		case "authorization_pending":
			return TokenResponse{}, tokenStatusPending, nil
		case "slow_down":
			return TokenResponse{}, tokenStatusSlowDown, nil
		case "expired_token":
			return TokenResponse{}, tokenStatusExpired, nil
		case "access_denied":
			return TokenResponse{}, tokenStatusDenied, nil
		default:
			return TokenResponse{}, tokenStatusUnknown,
				fmt.Errorf("device-token: %d %s", resp.StatusCode, payload.Error)
		}
	}

	return TokenResponse{}, tokenStatusUnknown, decodeStatus(resp, "device-token")
}

// decodeError builds an error for non-2xx responses on Start. It surfaces
// any `error` field from the JSON body when present.
func decodeError(resp *http.Response, op string) error {
	body, _ := io.ReadAll(resp.Body)
	var payload struct {
		Error string `json:"error"`
	}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &payload)
	}
	if payload.Error != "" {
		return fmt.Errorf("%s: %d %s", op, resp.StatusCode, payload.Error)
	}
	return fmt.Errorf("%s: unexpected status %d", op, resp.StatusCode)
}

// decodeStatus is the same as decodeError but for already-consumed bodies.
func decodeStatus(resp *http.Response, op string) error {
	return fmt.Errorf("%s: unexpected status %d", op, resp.StatusCode)
}
