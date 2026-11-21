package application

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckConnection(t *testing.T) {
	bootstrap := PipelineBootstrap{
		Cloud: "nube", Fingerprint: "aaa", Manifest: BootstrapManifest{Contract: "nube/v1"},
	}
	active := func(contract ContractID, fingerprint string) CloudConnectionInfo {
		return CloudConnectionInfo{
			Status: "active", BootstrapSHA256: fingerprint, Outputs: &ConnectionOutputs{ContractID: contract},
		}
	}

	t.Run("mismo contrato y misma huella", func(t *testing.T) {
		assert.NoError(t, CheckConnection(bootstrap, active("nube/v1", "aaa"), "sand"))
	})

	outdated := map[string]struct {
		conn CloudConnectionInfo
		want string
	}{
		"otro contrato":        {active("nube/v2", "aaa"), `la conexión es "nube/v2" y el pipeline pide "nube/v1"`},
		"otra huella":          {active("nube/v1", "bbb"), "otra versión de bootstrap/nube/"},
		"conexión sin huella":  {active("nube/v1", ""), "otra versión de bootstrap/nube/"},
		"conexión sin outputs": {CloudConnectionInfo{Status: "active", BootstrapSHA256: "aaa"}, "no describe"},
	}
	for name, tt := range outdated {
		t.Run(name, func(t *testing.T) {
			err := CheckConnection(bootstrap, tt.conn, "sand")

			require.ErrorIs(t, err, ErrBootstrapOutdated)
			assert.ErrorContains(t, err, "El bootstrap del pipeline cambió: ejecuta `vex bootstrap sand`")
			assert.ErrorContains(t, err, tt.want)
		})
	}
}
