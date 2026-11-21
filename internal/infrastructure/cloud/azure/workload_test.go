package azure

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jairoprogramador/vex/internal/application"
)

const tokenPath = "/vex/credenciales/oidc.jwt"

// backendFixture lee un fixture dorado del backend (la copia que valida el cliente del portal): lo que el
// portal acepta guardar es lo que este adaptador debe saber entregar.
func backendFixture(t *testing.T, name string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "portalclient", "testdata", "contract", name))
	require.NoError(t, err)
	var values map[string]string
	require.NoError(t, json.Unmarshal(data, &values))
	return values
}

func fullOutputs(t *testing.T) application.ConnectionOutputs {
	t.Helper()
	return application.ConnectionOutputs{
		ContractID: ContractV1,
		Params:     backendFixture(t, "azure-v1-params.json"),
		Extras:     backendFixture(t, "outputs-extras.json"),
	}
}

func TestAdapter_Contrato(t *testing.T) {
	adapter := New()

	assert.Equal(t, application.ContractID("azure/v1"), adapter.Contract())
	assert.Equal(t, []string{"ARM_", "AZURE_", "VEX_AZURE_"}, adapter.Workload().ReservedEnvPrefixes())
}

func TestEnv_ElConjuntoExactoDeVariables(t *testing.T) {
	outputs := fullOutputs(t)
	p := outputs.Params

	env, dropped, err := New().Env(outputs, tokenPath)

	require.NoError(t, err)
	assert.Empty(t, dropped)
	assert.Equal(t, map[string]string{
		// Identidad, con los nombres estándar de Azure.
		"AZURE_CLIENT_ID":            p["deploy_client_id"],
		"AZURE_TENANT_ID":            p["tenant_id"],
		"AZURE_SUBSCRIPTION_ID":      p["subscription_id"],
		"AZURE_FEDERATED_TOKEN_FILE": tokenPath,
		// La sesión de az, en el directorio que el motor borra al terminar cada paso.
		"AZURE_CONFIG_DIR": "/tmp/vex-step-tmp/azure",
		// Alias para las herramientas que leen ARM_*.
		"ARM_USE_OIDC":             "true",
		"ARM_CLIENT_ID":            p["deploy_client_id"],
		"ARM_TENANT_ID":            p["tenant_id"],
		"ARM_SUBSCRIPTION_ID":      p["subscription_id"],
		"ARM_OIDC_TOKEN_FILE_PATH": tokenPath,
		// Contexto de la conexión.
		"VEX_AZURE_RESOURCE_GROUP":          p["resource_group"],
		"VEX_AZURE_LOCATION":                p["location"],
		"VEX_AZURE_DEPLOY_IDENTITY_ID":      p["deploy_identity_id"],
		"VEX_AZURE_DEPLOY_PRINCIPAL_ID":     p["deploy_principal_id"],
		"VEX_AZURE_IDENTITY_RESOURCE_GROUP": p["identity_resource_group"],
		"VEX_AZURE_PULL_IDENTITY_ID":        p["pull_identity_id"],
		"VEX_AZURE_PULL_CLIENT_ID":          p["pull_client_id"],
		"VEX_AZURE_STATE_ACCOUNT":           p["state_account"],
		"VEX_AZURE_STATE_CONTAINER":         p["state_container"],
		// Salidas propias de la plantilla del pipeline.
		"VEX_BOOTSTRAP_CONTAINER_APPS_ENVIRONMENT_ID": outputs.Extras["containerAppsEnvironmentId"],
		"VEX_BOOTSTRAP_REGISTRY_LOGIN_SERVER":         outputs.Extras["registryLoginServer"],
	}, env)
	for name := range env {
		assert.False(t, strings.HasPrefix(name, "TF_"), "nada propio de una herramienta: %s", name)
	}
}

func TestEnv_SoloLoObligatorio(t *testing.T) {
	outputs := application.ConnectionOutputs{ContractID: ContractV1, Params: backendFixture(t, "azure-v1-params.minimal.json")}

	env, dropped, err := New().Env(outputs, tokenPath)

	require.NoError(t, err)
	assert.Empty(t, dropped)
	assert.Equal(t, []string{
		"ARM_CLIENT_ID", "ARM_OIDC_TOKEN_FILE_PATH", "ARM_SUBSCRIPTION_ID", "ARM_TENANT_ID", "ARM_USE_OIDC",
		"AZURE_CLIENT_ID", "AZURE_CONFIG_DIR", "AZURE_FEDERATED_TOKEN_FILE", "AZURE_SUBSCRIPTION_ID", "AZURE_TENANT_ID",
		"VEX_AZURE_DEPLOY_IDENTITY_ID", "VEX_AZURE_DEPLOY_PRINCIPAL_ID", "VEX_AZURE_LOCATION", "VEX_AZURE_RESOURCE_GROUP",
	}, slices.Sorted(maps.Keys(env)), "sin estado, sin identidad de pull y sin extras no hay más variables")
}

func TestEnv_LasClavesDesconocidasNoSeEntregan(t *testing.T) {
	outputs := fullOutputs(t)
	outputs.Params["zz_nuevo"] = "valor"
	outputs.Params["LD_PRELOAD"] = "/tmp/x.so"

	env, dropped, err := New().Env(outputs, tokenPath)

	require.NoError(t, err)
	assert.Equal(t, []string{"LD_PRELOAD", "zz_nuevo"}, dropped)
	for name, value := range env {
		assert.NotEqual(t, "LD_PRELOAD", name)
		assert.NotContains(t, value, "/tmp/x.so")
		assert.NotEqual(t, "valor", value)
	}
}

func TestEnv_RechazaLoQueNoCumpleElContrato(t *testing.T) {
	tests := map[string]struct {
		change func(application.ConnectionOutputs)
		want   string
	}{
		"falta un obligatorio":       {func(o application.ConnectionOutputs) { delete(o.Params, "deploy_identity_id") }, `falta "deploy_identity_id"`},
		"tenant que no es GUID":      {func(o application.ConnectionOutputs) { o.Params["tenant_id"] = "contoso.onmicrosoft.com" }, `"tenant_id"`},
		"suscripción con inyección":  {func(o application.ConnectionOutputs) { o.Params["subscription_id"] += " --debug" }, `"subscription_id"`},
		"client id vacío":            {func(o application.ConnectionOutputs) { o.Params["deploy_client_id"] = "" }, `"deploy_client_id"`},
		"principal que no es GUID":   {func(o application.ConnectionOutputs) { o.Params["deploy_principal_id"] = "x" }, `"deploy_principal_id"`},
		"identidad que no es de ARM": {func(o application.ConnectionOutputs) { o.Params["deploy_identity_id"] = "/etc/passwd" }, `"deploy_identity_id"`},
		"grupo con salto de línea":   {func(o application.ConnectionOutputs) { o.Params["resource_group"] = "rg\nPATH=/tmp" }, `"resource_group"`},
		"grupo con NUL":              {func(o application.ConnectionOutputs) { o.Params["resource_group"] = "rg\x00x" }, `"resource_group"`},
		"región con mayúsculas":      {func(o application.ConnectionOutputs) { o.Params["location"] = "East US" }, `"location"`},
		"opcional mal formado":       {func(o application.ConnectionOutputs) { o.Params["state_account"] = "Con-Guion" }, `"state_account"`},
		"valor larguísimo":           {func(o application.ConnectionOutputs) { o.Params["resource_group"] = strings.Repeat("a", 2000) }, `"resource_group"`},
		"pull sin su pareja":         {func(o application.ConnectionOutputs) { delete(o.Params, "pull_client_id") }, "sin su pareja"},
		"estado sin su pareja":       {func(o application.ConnectionOutputs) { delete(o.Params, "state_account") }, "sin su pareja"},
		"extra con nombre inválido":  {func(o application.ConnectionOutputs) { o.Extras["LD_PRELOAD"] = "x" }, "nombre no válido"},
		"extra con guion bajo":       {func(o application.ConnectionOutputs) { o.Extras["mi_salida"] = "x" }, "nombre no válido"},
		"extra de dos líneas":        {func(o application.ConnectionOutputs) { o.Extras["url"] = "a\nb" }, `salida extra "url"`},
		"extra con NUL":              {func(o application.ConnectionOutputs) { o.Extras["url"] = "a\x00b" }, `salida extra "url"`},
		"extra de 513 bytes":         {func(o application.ConnectionOutputs) { o.Extras["url"] = strings.Repeat("a", 513) }, `salida extra "url"`},
		"extra con forma de JWT":     {func(o application.ConnectionOutputs) { o.Extras["token"] = "eyJhbGciOiJSUzI1NiJ9.e30.c2ln" }, `salida extra "token"`},
		"demasiados extras": {func(o application.ConnectionOutputs) {
			for i := range 33 {
				o.Extras["salida"+strings.Repeat("x", i)] = "v"
			}
		}, "más de 32"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			outputs := fullOutputs(t)
			tt.change(outputs)

			env, _, err := New().Env(outputs, tokenPath)

			require.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
			assert.Nil(t, env, "ante un error no se entrega ninguna variable")
		})
	}
}

func TestEnv_ElErrorNoRepiteElValorRechazado(t *testing.T) {
	outputs := fullOutputs(t)
	outputs.Params["resource_group"] = "valor-secreto;rm -rf"

	_, _, err := New().Env(outputs, tokenPath)

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "valor-secreto")
}

func TestEnv_UnExtraDe512BytesExactosVale(t *testing.T) {
	outputs := fullOutputs(t)
	outputs.Extras["limite"] = strings.Repeat("x", 512)

	env, _, err := New().Env(outputs, tokenPath)

	require.NoError(t, err)
	assert.Len(t, env["VEX_BOOTSTRAP_LIMITE"], 512)
}

// El portal solo guarda extras en ASCII imprimible; lo que no lo sea no llega al contenedor.
func TestEnv_UnExtraQueNoEsASCIISeRechaza(t *testing.T) {
	outputs := fullOutputs(t)
	outputs.Extras["nombre"] = "señal"

	_, _, err := New().Env(outputs, tokenPath)

	require.Error(t, err)
}

// Los nombres de esta tabla son los que ya usan los commands.yaml de las plantillas.
func TestEnv_NombresDeLasVariablesDeLosExtras(t *testing.T) {
	tests := map[string]string{
		"url":                        "VEX_BOOTSTRAP_URL",
		"aksIdentityId":              "VEX_BOOTSTRAP_AKS_IDENTITY_ID",
		"sharedStateAccount":         "VEX_BOOTSTRAP_SHARED_STATE_ACCOUNT",
		"nodeResourceGroup":          "VEX_BOOTSTRAP_NODE_RESOURCE_GROUP",
		"registryLoginServer":        "VEX_BOOTSTRAP_REGISTRY_LOGIN_SERVER",
		"containerAppsEnvironmentId": "VEX_BOOTSTRAP_CONTAINER_APPS_ENVIRONMENT_ID",
		"subnet2Id":                  "VEX_BOOTSTRAP_SUBNET2_ID",
		"appURL":                     "VEX_BOOTSTRAP_APP_U_R_L",
	}
	for name, want := range tests {
		t.Run(name, func(t *testing.T) {
			outputs := application.ConnectionOutputs{
				ContractID: ContractV1, Params: backendFixture(t, "azure-v1-params.minimal.json"),
				Extras: map[string]string{name: "valor"},
			}

			env, _, err := New().Env(outputs, tokenPath)

			require.NoError(t, err)
			assert.Equal(t, "valor", env[want])
		})
	}
}

func TestExecutionContextKey(t *testing.T) {
	adapter := New()
	base := backendFixture(t, "azure-v1-params.json")
	key := adapter.ExecutionContextKey(base)

	for _, field := range []string{"tenant_id", "subscription_id", "resource_group", "location"} {
		changed := maps.Clone(base)
		changed[field] += "x"
		assert.NotEqual(t, key, adapter.ExecutionContextKey(changed), "cambiar %s es otro destino", field)
	}
	for _, field := range []string{"state_account", "deploy_client_id", "pull_identity_id"} {
		changed := maps.Clone(base)
		changed[field] += "x"
		assert.Equal(t, key, adapter.ExecutionContextKey(changed), "%s no cambia el destino", field)
	}
}

func TestErrorHints_SonUnaCopia(t *testing.T) {
	hints := New().ErrorHints()
	require.NotEmpty(t, hints)

	hints[0].Message = "otro"

	assert.NotEqual(t, "otro", New().ErrorHints()[0].Message)
}
