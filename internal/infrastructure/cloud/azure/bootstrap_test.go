package azure

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jairoprogramador/vex/internal/application"
)

const (
	testTenant    = "11111111-1111-4111-8111-111111111111"
	testProject   = "b5a1c2d4-0000-4000-8000-0000000000b1"
	testOperation = "vex-bootstrap-pagos-api-b5a1c2-sand"
)

// reply es lo que `az` contesta a un comando.
type reply struct {
	out string
	err error
}

// fakeAz es un `az` que no ejecuta nada: anota cada argv y contesta según el comando ("deployment sub what-if").
type fakeAz struct {
	replies   map[string]reply
	calls     [][]string
	templates []string // lo que había en --template-file en el momento de cada llamada
}

func (f *fakeAz) Run(_ context.Context, args ...string) ([]byte, error) {
	f.calls = append(f.calls, args)
	for i, arg := range args {
		if arg == "--template-file" {
			raw, err := os.ReadFile(args[i+1])
			if err != nil {
				return nil, err
			}
			f.templates = append(f.templates, string(raw))
		}
	}
	r := f.replies[commandLabel(args)]
	return []byte(r.out), r.err
}

// commands son los comandos ejecutados, sin opciones.
func (f *fakeAz) commands() []string {
	out := make([]string, len(f.calls))
	for i, call := range f.calls {
		out[i] = commandLabel(call)
	}
	return out
}

func testAccount() application.CloudAccount {
	return application.CloudAccount{Target: map[string]string{"tenant_id": testTenant, "subscription_id": testSubscription}}
}

// bootstrapRequest deja en una carpeta la plantilla base y devuelve la petición de bootstrap que la usa.
func bootstrapRequest(t *testing.T) application.BootstrapRequest {
	t.Helper()
	return application.BootstrapRequest{
		Account: testAccount(),
		Bootstrap: application.PipelineBootstrap{
			Dir: t.TempDir(), Manifest: application.BootstrapManifest{Template: "main.json"}, Template: encode(t, baseTemplate()),
		},
		Name: testOperation, ProjectID: testProject, ProjectSlug: "pagos-api-b5a1c2", Environment: "sand", Region: "eastus2",
	}
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return string(raw)
}

func TestAccount_LeeLaSesionSinTokens(t *testing.T) {
	az := &fakeAz{replies: map[string]reply{"account show": {out: `{
		"id": "22222222-2222-4222-8222-222222222222", "tenantId": "11111111-1111-4111-8111-111111111111",
		"name": "Pruebas\u001b[2K", "tenantDisplayName": "Ejemplo SA", "user": {"name": "ana@ejemplo.com", "type": "user"}}`}}}

	account, err := NewWithRunner(az).Bootstrap().Account(context.Background())

	require.NoError(t, err)
	assert.Equal(t, [][]string{{"account", "show", "--output", "json"}}, az.calls)
	assert.Equal(t, testAccount().Target, account.Target)
	assert.Equal(t, []application.LabeledValue{
		{Label: "Cuenta de Azure", Value: "ana@ejemplo.com"},
		{Label: "Directorio", Value: "Ejemplo SA (" + testTenant + ")"},
		{Label: "Suscripción", Value: `Pruebas\u{1b}[2K (` + testSubscription + ")"},
	}, account.Summary, "el nombre de la suscripción llega sin caracteres de control")
}

func TestAccount_Errores(t *testing.T) {
	tests := map[string]struct {
		reply reply
		want  string
	}{
		"sin az":                 {reply{err: ErrNotInstalled}, "no encuentro la CLI de Azure"},
		"sin sesión":             {reply{err: &CommandError{Command: "account show", Stderr: "Please run 'az login' to setup account."}}, "Ejecuta `az login`"},
		"otro fallo":             {reply{err: &CommandError{Command: "account show", Stderr: "boom"}}, "`az account show` falló: boom"},
		"no es JSON":             {reply{out: "WARNING"}, "no se entiende"},
		"sin suscripción válida": {reply{out: `{"id": "x", "tenantId": "y"}`}, "no devolvió una suscripción"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			az := &fakeAz{replies: map[string]reply{"account show": tt.reply}}

			_, err := NewWithRunner(az).Bootstrap().Account(context.Background())

			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func TestRegion(t *testing.T) {
	provider := New().Bootstrap()
	current := &application.ConnectionOutputs{Params: map[string]string{"location": "westeurope"}}

	region, err := provider.Region("", current)
	require.NoError(t, err)
	assert.Equal(t, "westeurope", region, "sin pedirla, la de la conexión")

	region, err = provider.Region("eastus2", current)
	require.NoError(t, err)
	assert.Equal(t, "eastus2", region)

	_, err = provider.Region("", nil)
	assert.ErrorContains(t, err, "--location")

	_, err = provider.Region("East US 2; rm", nil)
	assert.ErrorContains(t, err, "no es una región de Azure")
}

func TestRegisterProviders(t *testing.T) {
	t.Run("registra cada uno y espera", func(t *testing.T) {
		az := &fakeAz{}

		err := NewWithRunner(az).Bootstrap().RegisterProviders(context.Background(), testAccount(), []string{"Microsoft.App", "Microsoft.Storage"})

		require.NoError(t, err)
		assert.Equal(t, [][]string{
			{"provider", "register", "--namespace", "Microsoft.App", "--wait", "--subscription", testSubscription, "--output", "none"},
			{"provider", "register", "--namespace", "Microsoft.Storage", "--wait", "--subscription", testSubscription, "--output", "none"},
		}, az.calls)
	})
	t.Run("un nombre que no es un espacio de nombres no llega a az", func(t *testing.T) {
		for _, namespace := range []string{"--help", "Microsoft.App --debug", "Microsoft", "../x.y"} {
			az := &fakeAz{}

			err := NewWithRunner(az).Bootstrap().RegisterProviders(context.Background(), testAccount(), []string{"Microsoft.App", namespace})

			assert.ErrorContains(t, err, "no es un espacio de nombres de Azure", namespace)
			assert.Empty(t, az.calls, "se valida todo antes de registrar nada")
		}
	})
	t.Run("sesión sin suscripción válida", func(t *testing.T) {
		az := &fakeAz{}
		account := application.CloudAccount{Target: map[string]string{"subscription_id": "--debug", "tenant_id": testTenant}}

		err := NewWithRunner(az).Bootstrap().RegisterProviders(context.Background(), account, []string{"Microsoft.App"})

		require.Error(t, err)
		assert.Empty(t, az.calls)
	})
}

func deployArgs(path string, command ...string) []string {
	return append(command, "--template-file", path,
		"--name", testOperation, "--location", "eastus2", "--subscription", testSubscription,
		"--parameters", "projectId="+testProject, "projectSlug=pagos-api-b5a1c2", "environment=sand", "location=eastus2",
		"--no-prompt", "true", "--output", "json")
}

func templatePath(t *testing.T, call []string) string {
	t.Helper()
	for i, arg := range call {
		if arg == "--template-file" {
			return call[i+1]
		}
	}
	require.Fail(t, "sin --template-file")
	return ""
}

func TestPreview_EjecutaWhatIfConLaPlantillaComprobada(t *testing.T) {
	az := &fakeAz{replies: map[string]reply{"deployment sub what-if": {out: fixture(t, "whatif-create.json")}}}
	req := bootstrapRequest(t)

	preview, err := NewWithRunner(az).Bootstrap().Preview(context.Background(), req)

	require.NoError(t, err)
	require.Len(t, az.calls, 1)
	path := templatePath(t, az.calls[0])
	assert.Equal(t, deployArgs(path, "deployment", "sub", "what-if", "--result-format", "FullResourcePayloads", "--no-pretty-print"), az.calls[0])
	assert.NotEqual(t, filepath.Join(req.Bootstrap.Dir, "main.json"), path, "az lee una copia privada, no la carpeta del pipeline")
	assert.Equal(t, string(encode(t, baseTemplate())), az.templates[0])
	assert.NoFileExists(t, path, "la copia se borra")
	assert.Len(t, preview.Seal, 64)
	assert.Len(t, preview.Declared, 1)
	assert.True(t, preview.HasChanges())
	assert.Len(t, preview.Unpreviewed(), 1)
}

func TestPreview_NoLlamaAAzSiAlgoNoTieneFormato(t *testing.T) {
	tests := map[string]func(*application.BootstrapRequest){
		"nombre con opciones":     func(r *application.BootstrapRequest) { r.Name = "--debug" },
		"nombre de más de 64":     func(r *application.BootstrapRequest) { r.Name = "vex-bootstrap-" + strings.Repeat("a", 51) },
		"proyecto":                func(r *application.BootstrapRequest) { r.ProjectID = "x y=z" },
		"slug":                    func(r *application.BootstrapRequest) { r.ProjectSlug = "pagos api=1" },
		"ambiente":                func(r *application.BootstrapRequest) { r.Environment = "sand location=x" },
		"región":                  func(r *application.BootstrapRequest) { r.Region = "East US" },
		"suscripción":             func(r *application.BootstrapRequest) { r.Account.Target["subscription_id"] = "x" },
		"bootstrap sin plantilla": func(r *application.BootstrapRequest) { r.Bootstrap.Template = nil },
		"plantilla que rompe una regla": func(r *application.BootstrapRequest) {
			doc := baseTemplate()
			addResource(doc, map[string]any{"type": "Microsoft.Resources/deploymentScripts"})
			r.Bootstrap.Template = mustEncode(doc)
		},
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			az := &fakeAz{}
			req := bootstrapRequest(t)
			change(&req)

			_, err := NewWithRunner(az).Bootstrap().Preview(context.Background(), req)

			require.Error(t, err)
			assert.Empty(t, az.calls)
		})
	}
}

func TestPreview_UnaVistaPreviaQueRompeUnaReglaSeRechaza(t *testing.T) {
	doc := whatIf(t, "whatif-create.json")
	addChange(doc, change("Delete", testGroup, nil))
	az := &fakeAz{replies: map[string]reply{"deployment sub what-if": {out: string(encode(t, doc))}}}

	_, err := NewWithRunner(az).Bootstrap().Preview(context.Background(), bootstrapRequest(t))

	var rejected *application.BootstrapRejectedError
	require.ErrorAs(t, err, &rejected)
	assert.Equal(t, "V1", rejected.Rule)
}

func deploymentResult(outputs map[string]any) string {
	return string(mustEncode(map[string]any{"properties": map[string]any{"provisioningState": "Succeeded", "outputs": outputs}}))
}

func textOutput(value string) map[string]any { return map[string]any{"type": "String", "value": value} }

func validDeploymentOutputs() map[string]any {
	identities := "/subscriptions/" + testSubscription + "/resourceGroups/rg-pagos-sand-id/providers/Microsoft.ManagedIdentity/userAssignedIdentities/"
	return map[string]any{
		"deployIdentityId":      textOutput(identities + "id-pagos-sand-deploy"),
		"deployClientId":        textOutput("33333333-3333-4333-8333-333333333333"),
		"deployPrincipalId":     textOutput("44444444-4444-4444-8444-444444444444"),
		"resourceGroup":         textOutput("rg-pagos-sand"),
		"identityResourceGroup": textOutput("rg-pagos-sand-id"),
		"registryLoginServer":   textOutput("crpagossand.azurecr.io"),
		"replicas":              map[string]any{"type": "Int", "value": 2},
		"Nombre-Raro":           textOutput("x"),
	}
}

func sealOf(t *testing.T, req application.BootstrapRequest) string {
	t.Helper()
	az := &fakeAz{replies: map[string]reply{"deployment sub what-if": {out: fixture(t, "whatif-create.json")}}}
	preview, err := NewWithRunner(az).Bootstrap().Preview(context.Background(), req)
	require.NoError(t, err)
	return preview.Seal
}

func TestApply_DespliegaYDevuelveLaConexion(t *testing.T) {
	req := bootstrapRequest(t)
	seal := sealOf(t, req)
	az := &fakeAz{replies: map[string]reply{"deployment sub create": {out: deploymentResult(validDeploymentOutputs())}}}

	outputs, err := NewWithRunner(az).Bootstrap().Apply(context.Background(), req, seal)

	require.NoError(t, err)
	assert.Equal(t, deployArgs(templatePath(t, az.calls[0]), "deployment", "sub", "create"), az.calls[0])
	assert.Equal(t, application.ConnectionOutputs{
		ContractID: ContractV1,
		Params: map[string]string{
			"tenant_id": testTenant, "subscription_id": testSubscription, "location": "eastus2",
			"deploy_identity_id":      "/subscriptions/" + testSubscription + "/resourceGroups/rg-pagos-sand-id/providers/Microsoft.ManagedIdentity/userAssignedIdentities/id-pagos-sand-deploy",
			"deploy_client_id":        "33333333-3333-4333-8333-333333333333",
			"deploy_principal_id":     "44444444-4444-4444-8444-444444444444",
			"resource_group":          "rg-pagos-sand",
			"identity_resource_group": "rg-pagos-sand-id",
		},
		Extras: map[string]string{"registryLoginServer": "crpagossand.azurecr.io"},
	}, outputs, "las salidas que no son texto o no tienen un nombre válido no forman parte de la conexión")
}

func TestApply_SoloAplicaLoPrevisualizado(t *testing.T) {
	t.Run("la plantilla cambió después de la vista previa", func(t *testing.T) {
		req := bootstrapRequest(t)
		seal := sealOf(t, req)
		doc := baseTemplate()
		doc["contentVersion"] = "2.0.0.0"
		req.Bootstrap.Template = encode(t, doc)
		az := &fakeAz{}

		_, err := NewWithRunner(az).Bootstrap().Apply(context.Background(), req, seal)

		require.ErrorIs(t, err, application.ErrBootstrapChanged)
		assert.Empty(t, az.calls)
	})
	t.Run("cambió un parámetro", func(t *testing.T) {
		req := bootstrapRequest(t)
		seal := sealOf(t, req)
		req.Region = "westeurope"
		az := &fakeAz{}

		_, err := NewWithRunner(az).Bootstrap().Apply(context.Background(), req, seal)

		require.ErrorIs(t, err, application.ErrBootstrapChanged)
		assert.Empty(t, az.calls)
	})
	t.Run("sin sello", func(t *testing.T) {
		az := &fakeAz{}

		_, err := NewWithRunner(az).Bootstrap().Apply(context.Background(), bootstrapRequest(t), "")

		require.ErrorIs(t, err, application.ErrBootstrapChanged)
		assert.Empty(t, az.calls)
	})
}

func TestApply_SalidasQueNoSeAceptan(t *testing.T) {
	tests := map[string]struct {
		reply func(outputs map[string]any) reply
		want  string
	}{
		"az falla": {func(map[string]any) reply {
			return reply{err: &CommandError{Command: "deployment sub create", Stderr: "AuthorizationFailed"}}
		}, "AuthorizationFailed"},
		"el despliegue no terminó bien": {func(map[string]any) reply {
			return reply{out: `{"properties": {"provisioningState": "Failed"}}`}
		}, `estado "Failed"`},
		"no es JSON": {func(map[string]any) reply { return reply{out: "ok"} }, "no se entiende"},
		"salida segura": {func(o map[string]any) reply {
			o["clave"] = map[string]any{"type": "SecureString"}
			return reply{out: deploymentResult(o)}
		}, "salida segura"},
		"falta una obligatoria": {func(o map[string]any) reply {
			delete(o, "deployClientId")
			return reply{out: deploymentResult(o)}
		}, "no cumple el contrato azure/v1"},
		"un dato sin formato": {func(o map[string]any) reply {
			o["resourceGroup"] = textOutput("rg; rm -rf /")
			return reply{out: deploymentResult(o)}
		}, "no cumple el contrato azure/v1"},
		"identidad en otra suscripción": {func(o map[string]any) reply {
			o["deployIdentityId"] = textOutput("/subscriptions/99999999-9999-4999-8999-999999999999/resourceGroups/rg/providers/Microsoft.ManagedIdentity/userAssignedIdentities/id-ajena")
			return reply{out: deploymentResult(o)}
		}, "en otra suscripción"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			req := bootstrapRequest(t)
			seal := sealOf(t, req)
			az := &fakeAz{replies: map[string]reply{"deployment sub create": tt.reply(validDeploymentOutputs())}}

			_, err := NewWithRunner(az).Bootstrap().Apply(context.Background(), req, seal)

			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func TestSalidaDeAz_SeMuestraAcotadaYSinSecretos(t *testing.T) {
	stderr := "ERROR: \x1b[31mfalló\x1b[0m\r\nAuthorization: Bearer abc.def.ghi token eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJ4In0.firma " +
		"https://st.blob.core.windows.net/c?sig=SECRETO&x=1 " + strings.Repeat("relleno ", 400)

	text := clean(redact(stderr), maxErrorText)

	assert.NotContains(t, text, "\x1b")
	assert.NotContains(t, text, "\n")
	assert.NotContains(t, text, "abc.def.ghi")
	assert.NotContains(t, text, "eyJ")
	assert.NotContains(t, text, "SECRETO")
	assert.LessOrEqual(t, len([]rune(text)), maxErrorText+1)
	assert.True(t, strings.HasSuffix(text, "…"))
}

func TestClean_QuitaLoQueFalsificaLaTerminal(t *testing.T) {
	assert.Equal(t, "a b c", clean("  a\r\n\tb c  ", 20))
	assert.Equal(t, `a\u{0}\u{7}b`, clean("a\x00\x07b", 40), "los caracteres de control se ven, no actúan")
	assert.Equal(t, `\u{440}evisado \u{2714} m\u{f3}vil`, clean("\u0440evisado \u2714 móvil", 60), "solo ASCII: un homoglifo no pasa por una letra latina")
	assert.Equal(t, `rg\u{1b}[2K\u{202e}\u{200b}x`, clean("rg\x1b[2K\u202e\u200bx", 60), "ni ANSI, ni cambios de dirección, ni invisibles")
	assert.Equal(t, "abc…", clean("abcdef", 3))
}

func TestCommandLine_QuedaListoParaPegar(t *testing.T) {
	assert.Equal(t, "az group delete --name rg-pagos-sand", commandLine("group", "delete", "--name", "rg-pagos-sand"))
	assert.Equal(t, `az x 'a b' 'it'\''s' '$(id)' ''`, commandLine("x", "a b", "it's", "$(id)", ""))
}

func TestExecRunner_SinAzLoDice(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := execRunner{}.Run(context.Background(), "version")

	require.ErrorIs(t, err, ErrNotInstalled)
	assert.False(t, errors.Is(err, ErrNoSession))
}
