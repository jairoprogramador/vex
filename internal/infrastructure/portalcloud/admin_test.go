package portalcloud

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jairoprogramador/vex/internal/application"
)

const adminConnection = "c0ffee00-0000-4000-8000-0000000000c1"

// contract lee un fixture de contrato del backend (la copia del cliente del portal).
func contract(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "portalclient", "testdata", "contract", name))
	require.NoError(t, err)
	return string(data)
}

func TestCreate_DevuelveLoQueFijaElPortal(t *testing.T) {
	portal := newPortal(t, http.StatusOK, nil, contract(t, "cli-cloud-connection-create.response.json"))

	created, err := portal.Create(context.Background(), application.ConnectionDraft{
		ProjectID: "b5a1c2d4-0000-4000-8000-0000000000b1", Environment: "sand", Contract: "azure/v1",
		Target: map[string]string{"tenant_id": "t", "subscription_id": "s"},
	})

	require.NoError(t, err)
	assert.Equal(t, application.CreatedConnection{
		ConnectionID: adminConnection,
		Subject:      "vex:p:b5a1c2d4-0000-4000-8000-0000000000b1:e:sand:c:" + adminConnection,
		Issuer:       "https://id-dev.vexja.com", Audience: "api://AzureADTokenExchange",
		PendingExpiresAt: time.Date(2026, 11, 10, 12, 30, 0, 0, time.UTC),
		Project:          application.ConnectedProject{ID: "b5a1c2d4-0000-4000-8000-0000000000b1", Name: "Pagos API", Slug: "pagos-api-b5a1c2"},
		// B1: el texto libre del portal solo llega con ASCII imprimible; lo demás, escapado a la vista.
		Owner: application.ProjectOwner{Name: `Ana P\u{e9}rez`, Email: "ana@ejemplo.com"},
	}, created)
}

func TestCompleteYVerify_DevuelvenLaVerificacion(t *testing.T) {
	t.Run("verificándose: cuánto esperar y por qué", func(t *testing.T) {
		portal := newPortal(t, http.StatusOK, nil, contract(t, "cli-cloud-connection-complete.verifying.response.json"))

		v, err := portal.Complete(context.Background(), application.ConnectionCompletion{ConnectionID: adminConnection})

		require.NoError(t, err)
		assert.Equal(t, application.VerificationVerifying, v.Status)
		assert.Equal(t, 10*time.Second, v.RetryAfter)
		assert.Equal(t, "federation_not_found", v.DetailCode)
		assert.False(t, v.Conclusive())
	})
	t.Run("activa", func(t *testing.T) {
		portal := newPortal(t, http.StatusOK, nil, contract(t, "cli-cloud-connection-verify.response.json"))

		v, err := portal.Verify(context.Background(), adminConnection)

		require.NoError(t, err)
		assert.True(t, v.Conclusive())
	})
	t.Run("fallida, con lo que hay que ejecutar", func(t *testing.T) {
		portal := newPortal(t, http.StatusOK, nil, contract(t, "cli-cloud-connection-verify.failed.response.json"))

		v, err := portal.Verify(context.Background(), adminConnection)

		require.NoError(t, err)
		assert.Equal(t, application.VerificationFailed, v.Status)
		assert.Equal(t, "vex bootstrap sand --reconnect", v.Hint)
	})
}

func TestRevoke(t *testing.T) {
	t.Run("confirmada", func(t *testing.T) {
		portal := newPortal(t, http.StatusOK, nil, contract(t, "cli-cloud-connection-revoke.response.json"))

		require.NoError(t, portal.Revoke(context.Background(), adminConnection))
	})
	t.Run("sin confirmación es un error", func(t *testing.T) {
		portal := newPortal(t, http.StatusOK, nil, `{}`)

		assert.Error(t, portal.Revoke(context.Background(), adminConnection))
	})
}

func TestAdmin_ClasificaLosErrores(t *testing.T) {
	rejected := func(code, field string) func(*testing.T, error) {
		return func(t *testing.T, err error) {
			var r *application.ConnectionRejectedError
			require.ErrorAs(t, err, &r)
			assert.Equal(t, code, r.Code)
			assert.Equal(t, field, r.Field)
		}
	}
	tests := map[string]struct {
		status int
		header http.Header
		body   string
		check  func(*testing.T, error)
	}{
		"409 con código":                         {409, nil, `{"error":"connection_exists","hint":"vex bootstrap sand --reconnect"}`, rejected("connection_exists", "")},
		"403":                                    {403, nil, `{"error":"not_project_admin"}`, rejected("not_project_admin", "")},
		"404":                                    {404, nil, `{"error":"environment_not_found"}`, rejected("environment_not_found", "")},
		"400 con campo":                          {400, nil, `{"error":"invalid_param","field":"outputs.params.resource_group"}`, rejected("invalid_param", "outputs.params.resource_group")},
		"un campo con forma rara no se conserva": {400, nil, `{"error":"invalid_param","field":"x\u001b[2K y"}`, rejected("invalid_param", "")},
		"503 sin firma":                          {503, nil, `{"error":"signer_unavailable"}`, rejected("signer_unavailable", "")},
		"401: la sesión no vale": {401, nil, `{"error":"invalid_token"}`, func(t *testing.T, err error) {
			require.ErrorIs(t, err, application.ErrPortalSessionInvalid)
		}},
		"429: límite con su espera": {429, http.Header{"Retry-After": {"7"}}, `{"error":"rate_limited"}`, func(t *testing.T, err error) {
			require.ErrorIs(t, err, application.ErrCloudTokenThrottled)
			var retry application.RetryAfter
			require.ErrorAs(t, err, &retry)
			assert.Equal(t, 7*time.Second, retry.RetryAfter())
		}},
		"500: no es un rechazo": {500, nil, `{"error":"internal"}`, func(t *testing.T, err error) {
			var r *application.ConnectionRejectedError
			assert.False(t, errors.As(err, &r))
			assert.Error(t, err)
		}},
		"un código con forma rara no se conserva": {409, nil, `{"error":"Connection Exists!"}`, func(t *testing.T, err error) {
			var r *application.ConnectionRejectedError
			assert.False(t, errors.As(err, &r))
			assert.Error(t, err)
		}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			portal := newPortal(t, tt.status, tt.header, tt.body)

			_, err := portal.Verify(context.Background(), adminConnection)

			tt.check(t, err)
		})
	}
}
