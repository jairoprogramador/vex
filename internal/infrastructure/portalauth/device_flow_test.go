package portalauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDeviceFlowClient_Start(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    bool
		assertResp func(t *testing.T, resp DeviceCodeResponse)
	}{
		{
			name:       "success",
			statusCode: http.StatusOK,
			body: `{
				"device_code": "dev-123",
				"user_code": "WDJB-MJHT",
				"verification_uri": "https://portal/cli",
				"verification_uri_complete": "https://portal/cli?code=WDJB-MJHT",
				"expires_in": 600,
				"interval": 5
			}`,
			assertResp: func(t *testing.T, resp DeviceCodeResponse) {
				if resp.DeviceCode != "dev-123" {
					t.Fatalf("device_code: got %q", resp.DeviceCode)
				}
				if resp.UserCode != "WDJB-MJHT" {
					t.Fatalf("user_code: got %q", resp.UserCode)
				}
				if resp.Interval != 5 {
					t.Fatalf("interval: got %d", resp.Interval)
				}
			},
		},
		{
			name:       "server error",
			statusCode: http.StatusInternalServerError,
			body:       `{"error":"boom"}`,
			wantErr:    true,
		},
		{
			name:       "missing device_code",
			statusCode: http.StatusOK,
			body:       `{"user_code":"X"}`,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/functions/v1/device-code" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				if r.Method != http.MethodPost {
					t.Errorf("unexpected method: %s", r.Method)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			client := NewDeviceFlowClient(srv.URL)
			resp, err := client.Start(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.assertResp != nil {
				tt.assertResp(t, resp)
			}
		})
	}
}

func TestDeviceFlowClient_Poll(t *testing.T) {
	t.Parallel()

	t.Run("success after pending then slow_down", func(t *testing.T) {
		t.Parallel()

		var hits int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n := atomic.AddInt32(&hits, 1)
			w.Header().Set("Content-Type", "application/json")
			switch n {
			case 1:
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
			case 2:
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"slow_down"}`))
			default:
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"access_token":"tok-abc","token_type":"Bearer","expires_in":7776000}`))
			}
		}))
		defer srv.Close()

		client := NewDeviceFlowClient(srv.URL)
		// shrink the slow_down bump so the test does not actually wait 5s.
		client.slowDownStep = 1 * time.Millisecond
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// 1ms keeps the test fast; the server still drives state transitions.
		token, err := client.Poll(ctx, "dev-1", 1*time.Millisecond)
		if err != nil {
			t.Fatalf("Poll: %v", err)
		}
		if token.AccessToken != "tok-abc" {
			t.Fatalf("access_token: got %q", token.AccessToken)
		}
		if got := atomic.LoadInt32(&hits); got != 3 {
			t.Fatalf("expected 3 hits, got %d", got)
		}
	})

	t.Run("expired_token maps to ErrDeviceCodeExpired", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"expired_token"}`))
		}))
		defer srv.Close()

		client := NewDeviceFlowClient(srv.URL)
		_, err := client.Poll(context.Background(), "dev-1", 1*time.Millisecond)
		if !errors.Is(err, ErrDeviceCodeExpired) {
			t.Fatalf("expected ErrDeviceCodeExpired, got %v", err)
		}
	})

	t.Run("access_denied maps to ErrAccessDenied", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"access_denied"}`))
		}))
		defer srv.Close()

		client := NewDeviceFlowClient(srv.URL)
		_, err := client.Poll(context.Background(), "dev-1", 1*time.Millisecond)
		if !errors.Is(err, ErrAccessDenied) {
			t.Fatalf("expected ErrAccessDenied, got %v", err)
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
		}))
		defer srv.Close()

		client := NewDeviceFlowClient(srv.URL)
		ctx, cancel := context.WithCancel(context.Background())
		// Cancel almost immediately so the first <-time.After triggers ctx.Done.
		go func() {
			time.Sleep(20 * time.Millisecond)
			cancel()
		}()

		_, err := client.Poll(ctx, "dev-1", 50*time.Millisecond)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("transient network error retries until success", func(t *testing.T) {
		t.Parallel()

		var hits int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n := atomic.AddInt32(&hits, 1)
			w.Header().Set("Content-Type", "application/json")
			if n < 2 {
				// 503 with no JSON: classified as unknown -> transient retry.
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"access_token":"tok","token_type":"Bearer","expires_in":600}`))
		}))
		defer srv.Close()

		client := NewDeviceFlowClient(srv.URL)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		token, err := client.Poll(ctx, "dev-1", 1*time.Millisecond)
		if err != nil {
			t.Fatalf("Poll: %v", err)
		}
		if token.AccessToken != "tok" {
			t.Fatalf("access_token: got %q", token.AccessToken)
		}
	})
}

// Compile-time sanity: TokenResponse is JSON-decodable from the documented
// success body shape.
func TestTokenResponse_DecodeShape(t *testing.T) {
	t.Parallel()

	const body = `{"access_token":"a","token_type":"Bearer","expires_in":7776000}`
	var got TokenResponse
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "a" || got.TokenType != "Bearer" || got.ExpiresIn != 7776000 {
		t.Fatalf("unexpected decode: %+v", got)
	}
}
