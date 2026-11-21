package vos

import (
	"reflect"
	"strings"
	"testing"
)

func TestNewCloud(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		provider    string
		credentials string
		wantErr     string
	}{
		{name: "portal sin provider", credentials: CredentialsPortal},
		{name: "portal con provider", provider: "azure", credentials: CredentialsPortal},
		{name: "env con provider", provider: "aws", credentials: CredentialsEnv},
		{name: "credentials vacío", provider: "azure", wantErr: "cloud.credentials es obligatorio"},
		{name: "credentials inexistente", credentials: "vault", wantErr: `cloud.credentials "vault" no existe`},
		{name: "provider con mayúsculas", provider: "Azure", credentials: CredentialsPortal, wantErr: `cloud.provider "Azure"`},
		{name: "provider con símbolos", provider: "az-ure", credentials: CredentialsPortal, wantErr: `cloud.provider "az-ure"`},
		{name: "provider de una letra", provider: "a", credentials: CredentialsPortal, wantErr: `cloud.provider "a"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			cloud, err := NewCloud(c.provider, c.credentials)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("error: got %v, want que contenga %q", err, c.wantErr)
				}
				if !cloud.IsZero() {
					t.Fatalf("un bloque inválido debe devolver el valor cero, got %+v", cloud)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewCloud: %v", err)
			}
			if cloud.IsZero() || cloud.Provider() != c.provider || cloud.Credentials() != c.credentials {
				t.Fatalf("got %+v", cloud)
			}
			if cloud.IsPortal() != (c.credentials == CredentialsPortal) {
				t.Fatalf("IsPortal: got %v", cloud.IsPortal())
			}
		})
	}
}

func TestCloudZeroValue(t *testing.T) {
	t.Parallel()

	var cloud Cloud
	if !cloud.IsZero() || cloud.IsPortal() {
		t.Fatalf("el valor cero no es portal: %+v", cloud)
	}
}

func TestCloudConflictingEnvNames(t *testing.T) {
	t.Parallel()

	envs := mustEnvVars(t, "MI_VAR", "VEX_BOOTSTRAP_X", "ARM_CLIENT_SECRET", "VEX_CLOUD_CONTRACT")
	portal, _ := NewCloud("", CredentialsPortal)
	env, _ := NewCloud("", CredentialsEnv)

	cases := []struct {
		name     string
		cloud    Cloud
		prefixes []string
		want     []string
	}{
		{"solo neutrales", portal, NeutralReservedEnvPrefixes, []string{"VEX_BOOTSTRAP_X", "VEX_CLOUD_CONTRACT"}},
		{"con los del adaptador", portal, append([]string{"ARM_"}, NeutralReservedEnvPrefixes...),
			[]string{"VEX_BOOTSTRAP_X", "ARM_CLIENT_SECRET", "VEX_CLOUD_CONTRACT"}},
		{"sin prefijos", portal, nil, nil},
		{"credentials env", env, append([]string{"ARM_"}, NeutralReservedEnvPrefixes...), nil},
		{"sin bloque cloud", Cloud{}, NeutralReservedEnvPrefixes, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := c.cloud.ConflictingEnvNames(envs, c.prefixes); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}

	if got := portal.ConflictingEnvNames(mustEnvVars(t, "MI_VAR"), NeutralReservedEnvPrefixes); len(got) != 0 {
		t.Fatalf("una variable normal no es conflicto: %v", got)
	}
}

func mustEnvVars(t *testing.T, names ...string) []EnvVar {
	t.Helper()
	envs := make([]EnvVar, 0, len(names))
	for _, name := range names {
		env, err := NewEnvVar(name, "valor")
		if err != nil {
			t.Fatalf("NewEnvVar(%q): %v", name, err)
		}
		envs = append(envs, env)
	}
	return envs
}
