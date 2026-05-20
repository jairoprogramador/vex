// Package portalauth implements the OAuth 2.0 Device Authorization Grant
// flow against the Vex portal. It exposes a thin HTTP client for the
// device-code/device-token endpoints, a local file-based token store and a
// helper to open the verification URL in the user's browser.
package portalauth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DeviceCodeGrantType is the RFC 8628 grant type sent on the token endpoint.
const DeviceCodeGrantType = "urn:ietf:params:oauth:grant-type:device_code"

// CredentialsFileName is the name of the file that stores the persisted
// access token under the user's config directory.
const CredentialsFileName = "credentials.json"

// Sentinel errors returned by the device flow client and the token store.
// Callers (typically the auth subcommand) inspect them via errors.Is.
var (
	// ErrDeviceCodeExpired is returned by Poll when the server reports
	// `expired_token`. The user must restart the login flow.
	ErrDeviceCodeExpired = errors.New("portalauth: device code expired")

	// ErrAccessDenied is returned by Poll when the server reports
	// `access_denied`. The user explicitly rejected the device on the portal.
	ErrAccessDenied = errors.New("portalauth: access denied")

	// ErrTokenNotFound is returned by FileTokenStore.Load when no
	// credentials file exists yet (i.e. the user hasn't logged in).
	ErrTokenNotFound = errors.New("portalauth: token not found")
)

// Token is the persisted credential. ExpiresAt is computed from
// ObtainedAt + ExpiresIn at the time the CLI receives the access_token.
type Token struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpiresAt   time.Time `json:"expires_at"`
	ObtainedAt  time.Time `json:"obtained_at"`
}

// Expired reports whether the token has reached its expiration time.
// A zero ExpiresAt is treated as "no known expiration" (never expired).
func (t Token) Expired() bool {
	if t.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(t.ExpiresAt)
}

// DeviceCodeResponse mirrors the shape of the `device-code` edge function
// response (§6.1).
type DeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// TokenResponse mirrors the success body of the `device-token` edge
// function (§6.2).
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

func PortalURL() string  { return defaultPortalURL }
func BackendURL() string  { return defaultBackendURL }
func ClientID() string   { return defaultClientID }

func CredentialsPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve config directory: %w", err)
	}
	return filepath.Join(configDir, "vex", CredentialsFileName), nil
}
