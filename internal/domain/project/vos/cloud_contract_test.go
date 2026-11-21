package vos

import "testing"

func supports(known ...string) func(string) bool {
	return func(contract string) bool {
		for _, k := range known {
			if k == contract {
				return true
			}
		}
		return false
	}
}

func TestCloudFromContract(t *testing.T) {
	tests := []struct {
		name, contract string
		want           Cloud
		wantErr        bool
	}{
		{"sin contrato no hay bloque", "", Cloud{}, false},
		{"contrato conocido usa el portal", "azure/v1", Cloud{provider: "azure", credentials: CredentialsPortal}, false},
		{"contrato desconocido se rechaza", "azure/v2", Cloud{}, true},
		{"la comparación es exacta", "AZURE/v1", Cloud{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CloudFromContract(tt.contract, supports("azure/v1"))
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("CloudFromContract(%q) = %+v, %v", tt.contract, got, err)
			}
		})
	}
}

func TestReconcileCloud(t *testing.T) {
	portal := Cloud{provider: "azure", credentials: CredentialsPortal}
	env := Cloud{credentials: CredentialsEnv}
	tests := []struct {
		name     string
		actual   Cloud
		contract string
		want     Cloud
		wantErr  bool
	}{
		{"plantilla con contrato escribe el bloque", Cloud{}, "azure/v1", portal, false},
		{"plantilla sin contrato no escribe nada", Cloud{}, "", Cloud{}, false},
		{"no pisa credentials: env", env, "azure/v1", env, false},
		{"no pisa un bloque portal ya escrito", Cloud{credentials: CredentialsPortal}, "azure/v1", Cloud{credentials: CredentialsPortal}, false},
		{"plantilla sin contrato quita el portal", portal, "", Cloud{}, false},
		{"plantilla sin contrato conserva env", env, "", env, false},
		{"contrato desconocido se rechaza aunque haya bloque", env, "otra/v9", Cloud{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ReconcileCloud(tt.actual, tt.contract, supports("azure/v1"))
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("ReconcileCloud = %+v, %v; se esperaba %+v", got, err, tt.want)
			}
		})
	}
}
