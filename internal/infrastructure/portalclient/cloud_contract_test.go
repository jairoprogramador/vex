package portalclient

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// The files under testdata/contract are byte-for-byte copies of the backend's
// golden fixtures (vex-portal-backend: supabase/functions/_tests/fixtures/cloud).
// The backend tests produce them and these tests consume them: if either side
// drifts, one of the two suites fails. Refresh them by copying the directory.
func contractFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "contract", name))
	if err != nil {
		t.Fatalf("contract fixture %s: %v", name, err)
	}
	return data
}

func TestContract_CloudConnection_Active(t *testing.T) {
	t.Parallel()
	client, _ := newCloudServer(t, http.StatusOK, nil, contractFixture(t, "cli-cloud-connection.active.response.json"))

	resp, err := client.CloudConnection(context.Background(), "9d8d4777-5707-43b3-a636-d6f8433b7a34", "sand")
	if err != nil {
		t.Fatalf("the client rejects the backend's golden response: %v", err)
	}
	if resp.Status != "active" || resp.ConnectionID == "" || len(resp.BootstrapSHA256) != 64 || resp.PipelineCommit == "" {
		t.Fatalf("unexpected decode: %+v", resp)
	}
	if resp.Outputs == nil || resp.Outputs.ContractID != "azure/v1" {
		t.Fatalf("outputs: %+v", resp.Outputs)
	}
	for _, key := range []string{"tenant_id", "subscription_id", "deploy_client_id", "deploy_identity_id", "resource_group"} {
		if resp.Outputs.Params[key] == "" {
			t.Errorf("param %q missing after decoding", key)
		}
	}
}

func TestContract_CloudConnection_Absent(t *testing.T) {
	t.Parallel()
	client, _ := newCloudServer(t, http.StatusOK, nil, contractFixture(t, "cli-cloud-connection.absent.response.json"))

	resp, err := client.CloudConnection(context.Background(), "9d8d4777-5707-43b3-a636-d6f8433b7a34", "sand")
	if err != nil {
		t.Fatalf("the client rejects the backend's golden response: %v", err)
	}
	if resp.Status != "absent" || resp.Outputs != nil || resp.Hint == "" {
		t.Fatalf("unexpected decode: %+v", resp)
	}
}
