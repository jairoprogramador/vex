package portalclient

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	adminConnection = "c0ffee00-0000-4000-8000-0000000000c1"
	adminSHA        = "c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0"
)

func adminTarget() map[string]string {
	return map[string]string{
		"tenant_id": "11111111-1111-4111-8111-111111111111", "subscription_id": "22222222-2222-4222-8222-222222222222",
	}
}

func stringMap(t *testing.T, name string) map[string]string {
	t.Helper()
	var values map[string]string
	require.NoError(t, json.Unmarshal(contractFixture(t, name), &values))
	return values
}

// The request bodies the CLI sends are the ones the backend's contract fixtures describe.
func TestContract_CloudConnectionCreate(t *testing.T) {
	client, seen := newCloudServer(t, http.StatusOK, nil, contractFixture(t, "cli-cloud-connection-create.response.json"))

	resp, err := client.CreateCloudConnection(context.Background(), CloudConnectionCreateRequest{
		ProjectID: "b5a1c2d4-0000-4000-8000-0000000000b1", Environment: "sand", ContractID: "azure/v1",
		BootstrapSHA256: adminSHA, PipelineCommit: "0123abc", Target: adminTarget(),
	})

	require.NoError(t, err)
	assert.Equal(t, http.MethodPost, seen.method)
	assert.True(t, strings.HasSuffix(seen.path, "/cli-cloud-connection-create"), seen.path)
	assert.JSONEq(t, string(contractFixture(t, "cli-cloud-connection-create.request.json")), string(seen.body))
	assert.Equal(t, adminConnection, resp.ConnectionID)
	assert.False(t, resp.Resumed)
	assert.Empty(t, resp.ReplacesConnectionID)
	assert.Equal(t, "vex:p:b5a1c2d4-0000-4000-8000-0000000000b1:e:sand:c:"+adminConnection, resp.Subject)
	assert.Equal(t, "https://id-dev.vexja.com", resp.Issuer)
	assert.Equal(t, "api://AzureADTokenExchange", resp.Audience)
	assert.Equal(t, time.Date(2026, 11, 10, 12, 30, 0, 0, time.UTC), resp.PendingExpiresAt)
	assert.Equal(t, "Pagos API", resp.Project.Name)
	assert.Equal(t, "pagos-api-b5a1c2", resp.Project.Slug)
	require.NotNil(t, resp.Owner)
	assert.Equal(t, "ana@ejemplo.com", resp.Owner.Email)
	assert.False(t, resp.IsOwner)
}

func TestContract_CloudConnectionComplete(t *testing.T) {
	request := CloudConnectionCompleteRequest{
		ConnectionID: adminConnection, BootstrapSHA256: adminSHA, Target: adminTarget(),
		Outputs: CloudConnectionOutputs{
			ContractID: "azure/v1", Params: stringMap(t, "azure-v1-params.json"), Extras: stringMap(t, "outputs-extras.json"),
		},
	}

	t.Run("active", func(t *testing.T) {
		client, seen := newCloudServer(t, http.StatusOK, nil, contractFixture(t, "cli-cloud-connection-complete.response.json"))

		resp, err := client.CompleteCloudConnection(context.Background(), request)

		require.NoError(t, err)
		assert.True(t, strings.HasSuffix(seen.path, "/cli-cloud-connection-complete"), seen.path)
		assert.JSONEq(t, string(contractFixture(t, "cli-cloud-connection-complete.request.json")), string(seen.body))
		assert.Equal(t, "active", resp.Status)
	})
	t.Run("verifying", func(t *testing.T) {
		client, _ := newCloudServer(t, http.StatusOK, nil, contractFixture(t, "cli-cloud-connection-complete.verifying.response.json"))

		resp, err := client.CompleteCloudConnection(context.Background(), request)

		require.NoError(t, err)
		assert.Equal(t, "verifying", resp.Status)
		assert.Equal(t, 10000, resp.RetryAfterMS)
		assert.Equal(t, "federation_not_found", resp.DetailCode)
		assert.NotEmpty(t, resp.Message)
	})
	t.Run("without extras the field is omitted", func(t *testing.T) {
		client, seen := newCloudServer(t, http.StatusOK, nil, contractFixture(t, "cli-cloud-connection-complete.response.json"))
		minimal := request
		minimal.Outputs.Extras = nil

		_, err := client.CompleteCloudConnection(context.Background(), minimal)

		require.NoError(t, err)
		assert.NotContains(t, string(seen.body), `"extras"`)
	})
}

func TestContract_CloudConnectionVerify(t *testing.T) {
	t.Run("active", func(t *testing.T) {
		client, seen := newCloudServer(t, http.StatusOK, nil, contractFixture(t, "cli-cloud-connection-verify.response.json"))

		resp, err := client.VerifyCloudConnection(context.Background(), adminConnection)

		require.NoError(t, err)
		assert.True(t, strings.HasSuffix(seen.path, "/cli-cloud-connection-verify"), seen.path)
		assert.JSONEq(t, string(contractFixture(t, "cli-cloud-connection-verify.request.json")), string(seen.body))
		assert.Equal(t, "active", resp.Status)
	})
	t.Run("failed", func(t *testing.T) {
		client, _ := newCloudServer(t, http.StatusOK, nil, contractFixture(t, "cli-cloud-connection-verify.failed.response.json"))

		resp, err := client.VerifyCloudConnection(context.Background(), adminConnection)

		require.NoError(t, err)
		assert.Equal(t, "failed", resp.Status)
		assert.Equal(t, "vex bootstrap sand --reconnect", resp.Hint)
	})
}

func TestContract_CloudConnectionRevoke(t *testing.T) {
	client, seen := newCloudServer(t, http.StatusOK, nil, contractFixture(t, "cli-cloud-connection-revoke.response.json"))

	err := client.RevokeCloudConnection(context.Background(), adminConnection)

	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(seen.path, "/cli-cloud-connection-revoke"), seen.path)
	assert.JSONEq(t, string(contractFixture(t, "cli-cloud-connection-revoke.request.json")), string(seen.body))
}

// Revoking fails closed: only an explicit "revoked" counts.
func TestPortalClient_Revoke_FailsClosed(t *testing.T) {
	for name, body := range map[string]string{
		"empty object": `{}`, "another status": `{"status":"active"}`, "not json": `ok`,
	} {
		t.Run(name, func(t *testing.T) {
			client, _ := newCloudServer(t, http.StatusOK, nil, []byte(body))

			assert.Error(t, client.RevokeCloudConnection(context.Background(), adminConnection))
		})
	}
}

func TestPortalClient_CloudAdmin_RejectsWhatItDoesNotUnderstand(t *testing.T) {
	t.Run("incomplete connection on create", func(t *testing.T) {
		client, _ := newCloudServer(t, http.StatusOK, nil, []byte(`{"connection_id":"`+adminConnection+`","subject":"s","issuer":""}`))

		_, err := client.CreateCloudConnection(context.Background(), CloudConnectionCreateRequest{Target: adminTarget()})

		assert.ErrorContains(t, err, "conexión incompleta")
	})
	t.Run("unknown verification status", func(t *testing.T) {
		client, _ := newCloudServer(t, http.StatusOK, nil, []byte(`{"status":"ok"}`))

		_, err := client.VerifyCloudConnection(context.Background(), adminConnection)

		assert.ErrorContains(t, err, "estado desconocido")
	})
	t.Run("owner may be null", func(t *testing.T) {
		body := strings.Replace(string(contractFixture(t, "cli-cloud-connection-create.response.json")), `"owner": {`, `"owner": null, "x": {`, 1)
		client, _ := newCloudServer(t, http.StatusOK, nil, []byte(body))

		resp, err := client.CreateCloudConnection(context.Background(), CloudConnectionCreateRequest{Target: adminTarget()})

		require.NoError(t, err)
		assert.Nil(t, resp.Owner)
	})
}

// A target key cannot shadow a fixed field or carry an odd name.
func TestCloudAdmin_TargetNeverOverridesFixedFields(t *testing.T) {
	for _, key := range []string{"project_id", "reconnect", "Tenant", "a b", ""} {
		_, err := json.Marshal(CloudConnectionCreateRequest{Target: map[string]string{key: "x"}})
		assert.Error(t, err, key)
	}
	_, err := json.Marshal(CloudConnectionCompleteRequest{Target: map[string]string{"outputs": "x"}})
	assert.Error(t, err)
}

func TestPortalClient_CloudAdmin_ErrorsCarryCodeFieldAndHint(t *testing.T) {
	client, _ := newCloudServer(t, http.StatusBadRequest, nil,
		[]byte(`{"error":"invalid_param","field":"outputs.params.resource_group","hint":"vex bootstrap sand"}`))

	_, err := client.CompleteCloudConnection(context.Background(), CloudConnectionCompleteRequest{})

	var httpErr *HTTPError
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, "invalid_param", httpErr.Code)
	assert.Equal(t, "outputs.params.resource_group", httpErr.Field)
	assert.Equal(t, "vex bootstrap sand", httpErr.Hint)
}
