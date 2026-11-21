package azure

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jairoprogramador/vex/internal/application"
)

const (
	testIssuer     = "https://id.vexja.com"
	testConnection = "c0ffee00-0000-4000-8000-0000000000c1"
	otherConn      = "c0ffee00-0000-4000-8000-0000000000c2"
)

func subjectFor(environment, connection string) string {
	return "vex:p:" + testProject + ":e:" + environment + ":c:" + connection
}

func wantTrust() application.Trust {
	return application.Trust{Issuer: testIssuer, Subject: subjectFor("sand", testConnection), Audience: tokenExchangeAudience}
}

// federated es una credencial tal como la lista `az`.
func federated(name, issuer, subject string, audiences ...string) map[string]any {
	return map[string]any{"name": name, "issuer": issuer, "subject": subject, "audiences": audiences}
}

func listing(credentials ...map[string]any) reply {
	if credentials == nil {
		return reply{out: "[]"}
	}
	return reply{out: string(mustEncode(credentials))}
}

var identityArgs = []string{"--identity-name", "id-pagos-sand-deploy", "--resource-group", "rg-pagos-sand-id", "--subscription", testSubscription}

func credentialCommand(action string, extra ...string) []string {
	return append(append([]string{"identity", "federated-credential", action}, extra...), identityArgs...)
}

func TestTrust_AudienciaDelContrato(t *testing.T) {
	assert.Equal(t, "api://AzureADTokenExchange", New().Trust().Audience())
}

func TestEnsure(t *testing.T) {
	want := wantTrust()
	write := func(action string) []string {
		return append(credentialCommand(action, "--name", "vex-oidc"),
			"--issuer", testIssuer, "--subject", want.Subject, "--audiences", "api://AzureADTokenExchange", "--output", "none")
	}
	tests := map[string]struct {
		listed reply
		last   []string // el último comando; nil si solo se lista
		err    string
	}{
		"identidad sin credenciales: la crea": {listing(), write("create"), ""},
		"ya está la de esta conexión: no hace nada": {
			listing(federated("vex-oidc", testIssuer, want.Subject, tokenExchangeAudience)), nil, ""},
		"la de otra conexión del mismo proyecto y ambiente: la sustituye": {
			listing(federated("vex-oidc", testIssuer, subjectFor("sand", otherConn), tokenExchangeAudience)), write("update"), ""},
		"forma con properties (otras versiones de az)": {
			listing(map[string]any{"name": "vex-oidc", "properties": map[string]any{
				"issuer": testIssuer, "subject": want.Subject, "audiences": []string{tokenExchangeAudience}}}), nil, ""},
		"hay otra credencial: se detiene": {
			listing(federated("github\u001b[2K", "https://token.actions.githubusercontent.com", "repo:x/y", tokenExchangeAudience)),
			nil, `ya confía en otros emisores (github\u{1b}[2K)`},
		"vex-oidc con otro emisor: no la cambia": {
			listing(federated("vex-oidc", "https://id.atacante.test", want.Subject, tokenExchangeAudience)), nil, "vex no la cambia"},
		"vex-oidc de otro ambiente: no la cambia": {
			listing(federated("vex-oidc", testIssuer, subjectFor("prod", otherConn), tokenExchangeAudience)), nil, "vex no la cambia"},
		"vex-oidc con una audiencia de más: no la cambia": {
			listing(federated("vex-oidc", testIssuer, subjectFor("sand", otherConn), tokenExchangeAudience, "api://otra")), nil, "vex no la cambia"},
		"vex-oidc con un subject que no es de vex: no la cambia": {
			listing(federated("vex-oidc", testIssuer, "vex:p:"+testProject+":e:sand:c:*", tokenExchangeAudience)), nil, "vex no la cambia"},
		"la lista no se entiende": {reply{out: `{"value": []}`}, nil, "no se entiende"},
		"az falla al listar": {reply{err: &CommandError{Command: "identity federated-credential list", Stderr: "AuthorizationFailed"}},
			nil, "AuthorizationFailed"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			az := &fakeAz{replies: map[string]reply{"identity federated-credential list": tt.listed}}

			err := NewWithRunner(az).Trust().Ensure(context.Background(), testAccount(), fullOutputs(t), want)

			if tt.err != "" {
				assert.ErrorContains(t, err, tt.err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, append(credentialCommand("list"), "--output", "json"), az.calls[0])
			if tt.last == nil {
				assert.Len(t, az.calls, 1, "solo se lee")
				return
			}
			require.Len(t, az.calls, 2)
			assert.Equal(t, tt.last, az.calls[1])
		})
	}
}

func TestEnsure_NoLlamaAAzConUnaConfianzaOUnaIdentidadQueNoEsLaEsperada(t *testing.T) {
	tests := map[string]func(*application.Trust, *application.ConnectionOutputs, *application.CloudAccount){
		"emisor sin https": func(w *application.Trust, _ *application.ConnectionOutputs, _ *application.CloudAccount) {
			w.Issuer = "http://id.vexja.com"
		},
		"emisor con opción": func(w *application.Trust, _ *application.ConnectionOutputs, _ *application.CloudAccount) {
			w.Issuer = "--debug"
		},
		"subject con comodín": func(w *application.Trust, _ *application.ConnectionOutputs, _ *application.CloudAccount) {
			w.Subject = "vex:p:*"
		},
		"otra audiencia": func(w *application.Trust, _ *application.ConnectionOutputs, _ *application.CloudAccount) {
			w.Audience = "api://otra"
		},
		"identidad que no es un id de Azure": func(_ *application.Trust, o *application.ConnectionOutputs, _ *application.CloudAccount) {
			o.Params["deploy_identity_id"] = "id --resource-group otro"
		},
		"identidad en otra suscripción que la sesión": func(_ *application.Trust, _ *application.ConnectionOutputs, a *application.CloudAccount) {
			a.Target["subscription_id"] = "99999999-9999-4999-8999-999999999999"
		},
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			az := &fakeAz{}
			want, outputs, account := wantTrust(), fullOutputs(t), testAccount()
			change(&want, &outputs, &account)

			err := NewWithRunner(az).Trust().Ensure(context.Background(), account, outputs, want)

			require.Error(t, err)
			assert.Empty(t, az.calls)
		})
	}
}

func TestInspect(t *testing.T) {
	want := wantTrust()
	good := federated("vex-oidc", testIssuer, want.Subject, tokenExchangeAudience)
	tests := map[string]struct {
		listed   reply
		ok       []bool
		contains string
	}{
		"todo en orden":             {listing(good), []bool{true, true}, "tiene el emisor, el subject y la audiencia"},
		"falta la credencial":       {listing(), []bool{true, false}, "Falta la credencial federada vex-oidc"},
		"hay una credencial de más": {listing(good, federated("intruso", "https://x.test", "s", "a")), []bool{false, true}, "emisores que vex no puso (intruso)"},
		"cambiaron el emisor": {listing(federated("vex-oidc", "https://id.atacante.test", want.Subject, tokenExchangeAudience)),
			[]bool{true, false}, "el emisor es https://id.atacante.test y no el de vex"},
		"cambiaron el subject": {listing(federated("vex-oidc", testIssuer, subjectFor("sand", otherConn), tokenExchangeAudience)),
			[]bool{true, false}, "el subject no es el de esta conexión"},
		"añadieron una audiencia": {listing(federated("vex-oidc", testIssuer, want.Subject, tokenExchangeAudience, "api://otra")),
			[]bool{true, false}, "la audiencia no es api://AzureADTokenExchange"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			az := &fakeAz{replies: map[string]reply{"identity federated-credential list": tt.listed}}

			findings, err := NewWithRunner(az).Trust().Inspect(context.Background(), testAccount(), fullOutputs(t), want)

			require.NoError(t, err)
			require.Len(t, findings, len(tt.ok))
			var texts []string
			for i, finding := range findings {
				assert.Equal(t, tt.ok[i], finding.OK, finding.Text)
				texts = append(texts, finding.Text)
			}
			assert.Contains(t, strings.Join(texts, "\n"), tt.contains)
			assert.Equal(t, []string{"identity federated-credential list"}, az.commands(), "solo lee")
		})
	}
}

func TestRemove(t *testing.T) {
	want := wantTrust()
	remove := append(credentialCommand("delete", "--name", "vex-oidc"), "--yes")

	t.Run("borra la credencial de vex, sea de la conexión que sea", func(t *testing.T) {
		az := &fakeAz{replies: map[string]reply{"identity federated-credential list": listing(
			federated("vex-oidc", testIssuer, subjectFor("sand", otherConn), tokenExchangeAudience))}}

		err := NewWithRunner(az).Trust().Remove(context.Background(), testAccount(), fullOutputs(t), want)

		require.NoError(t, err)
		assert.Equal(t, remove, az.calls[1])
	})
	t.Run("si no está no hay nada que hacer", func(t *testing.T) {
		az := &fakeAz{replies: map[string]reply{"identity federated-credential list": listing()}}

		err := NewWithRunner(az).Trust().Remove(context.Background(), testAccount(), fullOutputs(t), want)

		require.NoError(t, err)
		assert.Len(t, az.calls, 1)
	})
	t.Run("una vex-oidc de otro emisor no se borra", func(t *testing.T) {
		az := &fakeAz{replies: map[string]reply{"identity federated-credential list": listing(
			federated("vex-oidc", "https://otro.test", "s", tokenExchangeAudience))}}

		err := NewWithRunner(az).Trust().Remove(context.Background(), testAccount(), fullOutputs(t), want)

		assert.ErrorContains(t, err, "no es de vex")
		assert.Len(t, az.calls, 1)
	})
}

func TestRemove_ConUnBloqueoNoLoQuitaYDiceLosComandosExactos(t *testing.T) {
	want := wantTrust()
	locked := reply{err: &CommandError{Command: "identity federated-credential delete",
		Stderr: "(ScopeLocked) The scope cannot perform delete operation because following scope(s) are locked"}}
	current := listing(federated("vex-oidc", testIssuer, want.Subject, tokenExchangeAudience))
	groupLocks := "/subscriptions/" + testSubscription + "/resourceGroups/rg-pagos-sand-id/providers/Microsoft.Authorization/locks/"
	scope := " --resource-group rg-pagos-sand-id --subscription " + testSubscription
	removeLine := "az identity federated-credential delete --name vex-oidc --identity-name id-pagos-sand-deploy" + scope + " --yes"

	t.Run("con el bloqueo del grupo", func(t *testing.T) {
		az := &fakeAz{replies: map[string]reply{
			"identity federated-credential list":   current,
			"identity federated-credential delete": locked,
			"lock list": {out: string(mustEncode([]map[string]any{
				{"name": "vex-protect-identities", "level": "CanNotDelete", "id": groupLocks + "vex-protect-identities"},
				{"name": "de-un-recurso", "level": "CanNotDelete", "id": "/subscriptions/" + testSubscription + "/resourceGroups/rg-pagos-sand-id/providers/Microsoft.Storage/storageAccounts/st/providers/Microsoft.Authorization/locks/de-un-recurso"},
				{"name": "x; rm -rf /", "level": "CanNotDelete", "id": groupLocks + "x"},
			}))},
		}}

		err := NewWithRunner(az).Trust().Remove(context.Background(), testAccount(), fullOutputs(t), want)

		var lockedErr *application.TrustLockedError
		require.ErrorAs(t, err, &lockedErr)
		assert.Equal(t, []string{
			"az lock delete --name vex-protect-identities" + scope,
			removeLine,
			"az lock create --name vex-protect-identities --lock-type CanNotDelete" + scope,
		}, lockedErr.Commands)
		assert.Equal(t, []string{"identity federated-credential list", "identity federated-credential delete", "lock list"}, az.commands(),
			"vex lee los bloqueos; no quita ninguno")
	})
	t.Run("si no puede leer los bloqueos da instrucciones genéricas", func(t *testing.T) {
		az := &fakeAz{replies: map[string]reply{
			"identity federated-credential list":   current,
			"identity federated-credential delete": locked,
			"lock list":                            {err: &CommandError{Command: "lock list"}},
		}}

		err := NewWithRunner(az).Trust().Remove(context.Background(), testAccount(), fullOutputs(t), want)

		var lockedErr *application.TrustLockedError
		require.ErrorAs(t, err, &lockedErr)
		require.Len(t, lockedErr.Commands, 3)
		assert.Equal(t, "az lock list"+scope+" --output table", lockedErr.Commands[0])
		assert.Equal(t, removeLine, lockedErr.Commands[2])
	})
}

func TestCleanup_SoloImprimeComandos(t *testing.T) {
	sub := " --subscription " + testSubscription

	guide := New().Trust().Cleanup(fullOutputs(t), testOperation)

	assert.Equal(t, []string{
		"az identity federated-credential delete --name vex-oidc --identity-name id-pagos-sand-deploy --resource-group rg-pagos-sand-id" + sub + " --yes",
	}, guide.RemoveTrust)
	assert.Equal(t, []string{
		"# roles que tiene la identidad de despliegue (los de fuera de sus grupos de recursos no se borran con ellos):",
		"az role assignment list --assignee 44444444-4444-4444-8444-444444444444 --all --output table" + sub,
		"# para quitar uno: az role assignment delete --ids <id>",
		"az group delete --name rg-pagos-sand" + sub,
		"az group delete --name rg-pagos-sand-id" + sub,
		"az deployment sub delete --name " + testOperation + sub,
		"# si un bloqueo impide borrar un grupo: az lock list --resource-group <grupo> y az lock delete --name <bloqueo> --resource-group <grupo>",
	}, guide.DeleteResources)

	t.Run("con una conexión que no cumple el contrato no propone nada", func(t *testing.T) {
		outputs := fullOutputs(t)
		outputs.Params["resource_group"] = "rg; rm -rf /"

		assert.Equal(t, application.CleanupGuide{}, New().Trust().Cleanup(outputs, testOperation))
	})
	t.Run("un nombre de operación raro no se imprime", func(t *testing.T) {
		guide := New().Trust().Cleanup(fullOutputs(t), "$(id)")

		assert.NotContains(t, strings.Join(guide.DeleteResources, "\n"), "$(id)")
	})
}

// Barandilla de la identidad (hallazgo A1 de la revisión): antes de crear la confianza, la identidad tiene que ser
// del bootstrap y no traer permisos que nadie vio.

const exposureGroup = "/subscriptions/" + testSubscription + "/resourceGroups/rg-pagos-sand"

func identityChange(t *testing.T, action application.ChangeAction) application.PlannedChange {
	t.Helper()
	return application.PlannedChange{
		Action: action, ID: fullOutputs(t).Params["deploy_identity_id"], Kind: "Microsoft.ManagedIdentity/userAssignedIdentities",
	}
}

func grantChange(action application.ChangeAction, id, identity, role, scope string) application.PlannedChange {
	return application.PlannedChange{
		Action: action, ID: id, Kind: "Microsoft.Authorization/roleAssignments",
		Grant: &application.PlannedGrant{Identity: identity, Role: role, Scope: scope},
	}
}

// effective es una asignación de rol tal como la lista `az role assignment list`.
func effective(scope, role string) map[string]any {
	return map[string]any{
		"id":               scope + "/providers/Microsoft.Authorization/roleAssignments/" + role,
		"scope":            scope,
		"roleDefinitionId": "/subscriptions/" + testSubscription + "/providers/Microsoft.Authorization/roleDefinitions/" + role,
		"principalId":      "44444444-4444-4444-8444-444444444444",
		"principalType":    "ServicePrincipal",
	}
}

func exposureAz(credentials reply, roles ...map[string]any) *fakeAz {
	listed := "[]"
	if len(roles) > 0 {
		listed = string(mustEncode(roles))
	}
	return &fakeAz{replies: map[string]reply{
		"identity federated-credential list": credentials,
		"role assignment list":               {out: listed},
	}}
}

func TestExposure_LaIdentidadDevueltaDebeEstarEnLaVistaPrevia(t *testing.T) {
	tests := map[string][]application.PlannedChange{
		"la vista previa no la menciona":                       nil,
		"Azure la ignora: existe y la plantilla no la declara": {identityChange(t, application.ChangeIgnored)},
		"no se pudo previsualizar":                             {identityChange(t, application.ChangeUnknown)},
		"otra identidad con otro nombre": {{
			Action: application.ChangeCreate, Kind: "Microsoft.ManagedIdentity/userAssignedIdentities",
			ID: "/subscriptions/" + testSubscription + "/resourceGroups/rg-pagos-sand-id/providers/Microsoft.ManagedIdentity/userAssignedIdentities/id-otra",
		}},
		"mismo identificador, otro tipo de recurso": {{
			Action: application.ChangeCreate, Kind: "Microsoft.Storage/storageAccounts", ID: fullOutputs(t).Params["deploy_identity_id"],
		}},
	}
	for name, changes := range tests {
		t.Run(name, func(t *testing.T) {
			az := &fakeAz{}

			_, err := NewWithRunner(az).Trust().Exposure(context.Background(), testAccount(),
				application.BootstrapPreview{Changes: changes}, fullOutputs(t), wantTrust())

			assert.Equal(t, "V6", rejectedRule(t, err))
			assert.Empty(t, az.calls, "se rechaza antes de hablar con Azure")
		})
	}
	t.Run("la identidad de descarga sigue la misma regla", func(t *testing.T) {
		outputs := fullOutputs(t)
		require.Contains(t, outputs.Params, "pull_identity_id")

		_, err := NewWithRunner(&fakeAz{}).Trust().Exposure(context.Background(), testAccount(),
			application.BootstrapPreview{Changes: []application.PlannedChange{identityChange(t, application.ChangeCreate)}}, outputs, wantTrust())

		assert.Equal(t, "V6", rejectedRule(t, err))
		assert.ErrorContains(t, err, outputs.Params["pull_identity_id"])
	})
}

// exposurePreview es una vista previa que crea (o mantiene) las dos identidades de fullOutputs y, además, los
// cambios que se le pasen.
func exposurePreview(t *testing.T, action application.ChangeAction, extra ...application.PlannedChange) application.BootstrapPreview {
	t.Helper()
	pull := identityChange(t, action)
	pull.ID = fullOutputs(t).Params["pull_identity_id"]
	return application.BootstrapPreview{Changes: append([]application.PlannedChange{identityChange(t, action), pull}, extra...)}
}

func TestExposure_RolesQueLaVistaPreviaNoMostro(t *testing.T) {
	const identity = "identidad id-pagos-sand-deploy"
	contributor := effective(exposureGroup, contributorRole)
	owner := effective("/subscriptions/"+testSubscription, ownerRole)
	previewed := grantChange(application.ChangeCreate, "", "identidad por crear [reference(…)]", "Contributor", "grupo de recursos rg-pagos-sand")
	byID := grantChange(application.ChangeNone, contributor["id"].(string), identity, "Contributor", "grupo de recursos rg-pagos-sand")
	vexCredential := listing(federated("vex-oidc", testIssuer, subjectFor("sand", otherConn), tokenExchangeAudience))

	t.Run("identidad recién creada con los roles que mostró la vista previa: nada que aceptar", func(t *testing.T) {
		az := exposureAz(listing(), contributor)

		exposed, err := NewWithRunner(az).Trust().Exposure(context.Background(), testAccount(),
			exposurePreview(t, application.ChangeCreate, previewed), fullOutputs(t), wantTrust())

		require.NoError(t, err)
		assert.Empty(t, exposed)
		assert.Equal(t, []string{"identity federated-credential list", "role assignment list"}, az.commands(), "solo lee")
		assert.Equal(t, []string{"role", "assignment", "list", "--assignee-object-id", "44444444-4444-4444-8444-444444444444", "--all",
			"--fill-principal-name", "false", "--fill-role-definition-name", "false", "--subscription", testSubscription, "--output", "json"}, az.calls[1])
	})
	t.Run("un rol que nadie declaró se devuelve, resaltado si es privilegiado", func(t *testing.T) {
		az := exposureAz(listing(), contributor, owner)

		exposed, err := NewWithRunner(az).Trust().Exposure(context.Background(), testAccount(),
			exposurePreview(t, application.ChangeCreate, previewed), fullOutputs(t), wantTrust())

		require.NoError(t, err)
		require.Len(t, exposed, 1)
		assert.Equal(t, application.PlannedGrant{
			Identity: identity, Role: "Owner", Scope: "toda la suscripción", Elevated: true,
			Reason: "da control sobre los permisos; alcanza toda la suscripción; la vista previa no lo mostró",
		}, exposed[0])
	})
	t.Run("un rol corriente sin declarar también se devuelve", func(t *testing.T) {
		az := exposureAz(listing(), effective(exposureGroup+"-otro", contributorRole))

		exposed, err := NewWithRunner(az).Trust().Exposure(context.Background(), testAccount(),
			exposurePreview(t, application.ChangeCreate, previewed), fullOutputs(t), wantTrust())

		require.NoError(t, err)
		require.Len(t, exposed, 1)
		assert.False(t, exposed[0].Elevated)
		assert.Equal(t, "la vista previa no lo mostró", exposed[0].Reason)
	})
	t.Run("identidad que ya existía y aún no confiaba en vex: se devuelven todos sus roles", func(t *testing.T) {
		// El caso del hallazgo: la plantilla declara una identidad que ya existe, con roles que también declara.
		az := exposureAz(listing(), contributor)

		exposed, err := NewWithRunner(az).Trust().Exposure(context.Background(), testAccount(),
			exposurePreview(t, application.ChangeNone, byID), fullOutputs(t), wantTrust())

		require.NoError(t, err)
		require.Len(t, exposed, 1)
		assert.Contains(t, exposed[0].Reason, "ya existía antes de este bootstrap")
	})
	t.Run("reconexión: la identidad ya confiaba en vex y solo cuentan los roles sin declarar", func(t *testing.T) {
		az := exposureAz(vexCredential, contributor, owner)

		exposed, err := NewWithRunner(az).Trust().Exposure(context.Background(), testAccount(),
			exposurePreview(t, application.ChangeNone, byID), fullOutputs(t), wantTrust())

		require.NoError(t, err)
		require.Len(t, exposed, 1)
		assert.Equal(t, "Owner", exposed[0].Role)
	})
	t.Run("un rol que Azure ignora no cuenta como declarado", func(t *testing.T) {
		ignored := byID
		ignored.Action = application.ChangeIgnored
		az := exposureAz(vexCredential, contributor)

		exposed, err := NewWithRunner(az).Trust().Exposure(context.Background(), testAccount(),
			exposurePreview(t, application.ChangeNone, ignored), fullOutputs(t), wantTrust())

		require.NoError(t, err)
		assert.Len(t, exposed, 1)
	})
	t.Run("el mismo rol para otra identidad no cuenta como declarado", func(t *testing.T) {
		other := grantChange(application.ChangeCreate, "", "identidad id-otra", "Contributor", "grupo de recursos rg-pagos-sand")
		az := exposureAz(listing(), contributor)

		exposed, err := NewWithRunner(az).Trust().Exposure(context.Background(), testAccount(),
			exposurePreview(t, application.ChangeCreate, other), fullOutputs(t), wantTrust())

		require.NoError(t, err)
		assert.Len(t, exposed, 1)
	})
}

func TestExposure_SiNoSePuedenLeerLosRolesNoSeSigue(t *testing.T) {
	for name, roles := range map[string]reply{
		"az falla":             {err: &CommandError{Command: "role assignment list", Stderr: "AuthorizationFailed"}},
		"no es una lista":      {out: `{"value": []}`},
		"rol sin id":           {out: `[{"scope": "/subscriptions/` + testSubscription + `", "roleDefinitionId": "x", "principalId": "44444444-4444-4444-8444-444444444444"}]`},
		"es de otro principal": {out: string(mustEncode([]any{map[string]any{"scope": exposureGroup, "principalId": "99999999-9999-4999-8999-999999999999", "roleDefinitionId": effective(exposureGroup, contributorRole)["roleDefinitionId"]}}))},
	} {
		t.Run(name, func(t *testing.T) {
			az := &fakeAz{replies: map[string]reply{"identity federated-credential list": listing(), "role assignment list": roles}}

			_, err := NewWithRunner(az).Trust().Exposure(context.Background(), testAccount(),
				exposurePreview(t, application.ChangeCreate), fullOutputs(t), wantTrust())

			require.Error(t, err, "sin saber qué puede hacer la identidad no se crea la confianza")
		})
	}
}
