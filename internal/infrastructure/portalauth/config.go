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
	"runtime"
	"time"
)

// DefaultPortalURL is used when the VEX_PORTAL_URL environment variable is
// not set. It points to the canonical hosted Vex portal.
const DefaultPortalURL = "https://vexportal.app"

// ClientID is the OAuth client identifier registered for the CLI in the
// portal's edge functions (§6.1, §6.2).
const ClientID = "vex-cli"

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

// PortalURL resolves the portal base URL using the VEX_PORTAL_URL
// environment variable, falling back to DefaultPortalURL.
func PortalURL() string {
	if url := os.Getenv("VEX_PORTAL_URL"); url != "" {
		return url
	}
	return DefaultPortalURL
}

// CredentialsPath returns the absolute path to the credentials file.
//
// Resolution order:
//  1. $XDG_CONFIG_HOME/.vex/credentials.json (Linux/macOS, when set).
//  2. %APPDATA%\.vex\credentials.json on Windows.
//  3. $HOME/.vex/.config/credentials.json on POSIX as last resort.
func CredentialsPath() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, ".vex", CredentialsFileName), nil
	}
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, ".vex", CredentialsFileName), nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".vex", ".config", CredentialsFileName), nil
}
