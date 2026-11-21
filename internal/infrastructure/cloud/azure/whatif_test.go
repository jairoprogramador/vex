package azure

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jairoprogramador/vex/internal/application"
)

const (
	testSubscription = "22222222-2222-4222-8222-222222222222"
	testGroup        = "/subscriptions/" + testSubscription + "/resourceGroups/rg-vex-pagos-api-b5a1c2-sand"
	ownerRole        = "8e3af657-a8ff-443c-a75c-2fe8c4bcb635"
	contributorRole  = "b24988ac-6180-42a0-ab88-20f7382dd24c"
)

// whatIf carga un fixture (sintético: ver testdata/README.md) para manipularlo.
func whatIf(t *testing.T, name string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(raw, &doc))
	return doc
}

// previewOf lee una vista previa con la suscripción de prueba como la de la sesión.
func previewOf(raw []byte) ([]application.PlannedChange, error) {
	return parseWhatIf(raw, testSubscription)
}

func addChange(doc map[string]any, change map[string]any) {
	doc["changes"] = append(doc["changes"].([]any), change)
}

func change(kind, id string, after map[string]any) map[string]any {
	return map[string]any{"changeType": kind, "resourceId": id, "after": after, "before": nil}
}

func assignment(scope, role, principal string) map[string]any {
	id := scope + "/providers/Microsoft.Authorization/roleAssignments/bbbbbbbb-1111-4111-8111-111111111111"
	return change("Create", id, map[string]any{
		"type": "Microsoft.Authorization/roleAssignments",
		"properties": map[string]any{
			"roleDefinitionId": "/subscriptions/" + testSubscription + "/providers/Microsoft.Authorization/roleDefinitions/" + role,
			"principalId":      principal,
		},
	})
}

func count(changes []application.PlannedChange, action application.ChangeAction) int {
	n := 0
	for _, c := range changes {
		if c.Action == action {
			n++
		}
	}
	return n
}

func TestVistaPrevia_PrimeraVez(t *testing.T) {
	changes, err := previewOf(encode(t, whatIf(t, "whatif-create.json")))

	require.NoError(t, err)
	assert.Equal(t, 9, count(changes, application.ChangeCreate))
	assert.Equal(t, 1, count(changes, application.ChangeUnknown), "lo que Azure no supo previsualizar no se esconde")
	var grants []string
	for _, c := range changes {
		if c.Grant != nil {
			grants = append(grants, c.Name)
			assert.False(t, c.Grant.Elevated)
		}
		if c.Action == application.ChangeUnknown {
			assert.Equal(t, "The resource name could not be evaluated before deployment.", c.Note)
		}
	}
	assert.ElementsMatch(t, []string{
		"identidad id-vex-pagos-api-b5a1c2-sand-deploy → Contributor → grupo de recursos rg-vex-pagos-api-b5a1c2-sand",
		"identidad id-vex-pagos-api-b5a1c2-sand-pull → AcrPull → grupo de recursos rg-vex-pagos-api-b5a1c2-sand",
		"identidad id-vex-pagos-api-b5a1c2-sand-deploy → Managed Identity Operator → grupo de recursos rg-vex-pagos-api-b5a1c2-sand-id › userAssignedIdentities/id-vex-pagos-api-b5a1c2-sand-pull",
	}, grants)
}

func TestVistaPrevia_SegundaVezNoCambiaNada(t *testing.T) {
	changes, err := previewOf(encode(t, whatIf(t, "whatif-nochange.json")))

	require.NoError(t, err, "la credencial vex-oidc que Azure ignora no rompe V2: la creó vex y la plantilla no la toca")
	preview := application.BootstrapPreview{Changes: changes}
	assert.False(t, preview.HasChanges())
	assert.Equal(t, 9, count(changes, application.ChangeNone))
	assert.Equal(t, 2, count(changes, application.ChangeIgnored))
	assert.Empty(t, preview.ElevatedGrants())
}

func TestVistaPrevia_Reglas(t *testing.T) {
	federated := testGroup + "-id/providers/Microsoft.ManagedIdentity/userAssignedIdentities/id-deploy/federatedIdentityCredentials/intruso"
	tests := map[string]struct {
		rule   string
		change func(doc map[string]any)
	}{
		"V1 borra un recurso": {"V1", func(doc map[string]any) { addChange(doc, change("Delete", testGroup, nil)) }},
		"V1 borra entre los cambios posibles": {"V1", func(doc map[string]any) {
			doc["potentialChanges"] = []any{change("Delete", testGroup, nil)}
		}},
		"V2 crea una credencial federada": {"V2", func(doc map[string]any) { addChange(doc, change("Create", federated, map[string]any{})) }},
		"V2 credencial federada sin cambios (está en la plantilla)": {"V2", func(doc map[string]any) {
			addChange(doc, change("NoChange", federated, map[string]any{}))
		}},
		"V2 credencial federada sin previsualizar": {"V2", func(doc map[string]any) { addChange(doc, change("Unsupported", federated, nil)) }},
		"V2 el tipo solo aparece en la descripción": {"V2", func(doc map[string]any) {
			addChange(doc, change("Create", testGroup+"/providers/Microsoft.Storage/storageAccounts/st1",
				map[string]any{"type": "Microsoft.ManagedIdentity/userAssignedIdentities/FederatedIdentityCredentials"}))
		}},
		"V2 script de despliegue": {"V2", func(doc map[string]any) {
			addChange(doc, change("Create", testGroup+"/providers/Microsoft.Resources/deploymentScripts/correr", map[string]any{}))
		}},
		"V4 tipo de cambio desconocido":   {"V4", func(doc map[string]any) { addChange(doc, change("Replace", testGroup, map[string]any{})) }},
		"V4 campo desconocido en la raíz": {"V4", func(doc map[string]any) { doc["moreChanges"] = []any{} }},
		"V4 campo desconocido en un cambio": {"V4", func(doc map[string]any) {
			c := change("Create", testGroup, map[string]any{})
			c["sideEffects"] = []any{}
			addChange(doc, c)
		}},
		"V4 la vista previa no terminó bien": {"V4", func(doc map[string]any) { doc["status"] = "Failed" }},
		"V4 la vista previa trae un error":   {"V4", func(doc map[string]any) { doc["error"] = map[string]any{"code": "InvalidTemplate"} }},
		"V4 sin lista de cambios":            {"V4", func(doc map[string]any) { delete(doc, "changes") }},
		"V4 identificador que no es de Azure": {"V4", func(doc map[string]any) {
			addChange(doc, change("Create", "/subscriptions/"+testSubscription+"/resourceGroups/rg/../../x", map[string]any{}))
		}},
		"V4 identificador con caracteres de control": {"V4", func(doc map[string]any) {
			addChange(doc, change("Create", testGroup+"/providers/Microsoft.Storage/storageAccounts/st\u001b[2K", map[string]any{}))
		}},
		"V4 crear sin descripción": {"V4", func(doc map[string]any) { addChange(doc, change("Create", testGroup+"x", nil)) }},
		"V4 rol sin resolver": {"V4", func(doc map[string]any) {
			a := assignment(testGroup, contributorRole, "33333333-3333-4333-8333-333333333333")
			a["after"].(map[string]any)["properties"].(map[string]any)["roleDefinitionId"] = "[variables('rol')]"
			addChange(doc, a)
		}},
		"V4 ámbito que no coincide con el identificador": {"V4", func(doc map[string]any) {
			a := assignment(testGroup, contributorRole, "33333333-3333-4333-8333-333333333333")
			a["after"].(map[string]any)["properties"].(map[string]any)["scope"] = "/subscriptions/" + testSubscription
			addChange(doc, a)
		}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			doc := whatIf(t, "whatif-create.json")
			tt.change(doc)

			_, err := previewOf(encode(t, doc))

			var rejected *application.BootstrapRejectedError
			require.ErrorAs(t, err, &rejected)
			assert.Equal(t, tt.rule, rejected.Rule, err.Error())
		})
	}
}

const (
	ownPrincipal     = "33333333-3333-4333-8333-333333333333"
	foreignPrincipal = "99999999-9999-4999-8999-999999999999"
)

// ownSetup es una vista previa en la que el bootstrap crea un grupo nuevo y mantiene una identidad suya, cuyo
// principal ya se conoce.
func ownSetup(extra ...map[string]any) map[string]any {
	identity := change("NoChange", testGroup+"-id/providers/Microsoft.ManagedIdentity/userAssignedIdentities/id-deploy",
		map[string]any{"type": "Microsoft.ManagedIdentity/userAssignedIdentities", "properties": map[string]any{"principalId": ownPrincipal}})
	changes := []any{change("Create", testGroup, map[string]any{}), change("NoChange", testGroup+"-id", map[string]any{}), identity}
	for _, c := range extra {
		changes = append(changes, c)
	}
	return map[string]any{"status": "Succeeded", "changes": changes}
}

func TestVistaPrevia_PermisosResaltados(t *testing.T) {
	const group = "grupo de recursos rg-vex-pagos-api-b5a1c2-sand"
	subscription := "/subscriptions/" + testSubscription
	tests := map[string]struct {
		change   map[string]any
		name     string
		elevated bool
	}{
		"Contributor para una identidad del bootstrap sobre un grupo que crea": {assignment(testGroup, contributorRole, ownPrincipal),
			"identidad id-deploy → Contributor → " + group, false},
		"Owner en un grupo": {assignment(testGroup, ownerRole, ownPrincipal), "identidad id-deploy → Owner → " + group, true},
		"User Access Administrator": {assignment(testGroup, "18d7d88d-d35e-4fb4-a5c3-7773c20a72d9", ownPrincipal),
			"identidad id-deploy → User Access Administrator → " + group, true},
		"RBAC Administrator": {assignment(testGroup, "f58310d9-a9f6-439a-9e8d-f62e7b41a168", ownPrincipal),
			"identidad id-deploy → Role Based Access Control Administrator → " + group, true},
		"Contributor en la suscripción": {assignment(subscription, contributorRole, ownPrincipal),
			"identidad id-deploy → Contributor → toda la suscripción", true},
		"rol a medida": {assignment(testGroup, "11111111-2222-4333-8444-555555555555", ownPrincipal),
			"identidad id-deploy → rol 11111111-2222-4333-8444-555555555555 → " + group, true},
		// M4: Contributor solo es «conocido» para una identidad del bootstrap y sobre algo que el bootstrap crea.
		"Contributor para una identidad ajena": {assignment(testGroup, contributorRole, foreignPrincipal),
			"identidad " + foreignPrincipal + " → Contributor → " + group, true},
		"Contributor sobre un grupo que ya existía": {assignment(testGroup+"-id", contributorRole, ownPrincipal),
			"identidad id-deploy → Contributor → " + group + "-id", true},
		"Managed Identity Operator sobre una identidad que ya existía": {
			assignment(testGroup+"-id/providers/Microsoft.ManagedIdentity/userAssignedIdentities/id-deploy", "f1a07417-d97a-45cb-824c-7a7467783830", ownPrincipal),
			"identidad id-deploy → Managed Identity Operator → " + group + "-id › userAssignedIdentities/id-deploy", true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			changes, err := previewOf(encode(t, ownSetup(tt.change)))

			require.NoError(t, err)
			var grant *application.PlannedChange
			for i := range changes {
				if changes[i].Grant != nil {
					grant = &changes[i]
				}
			}
			require.NotNil(t, grant)
			assert.Equal(t, tt.name, grant.Name)
			assert.Equal(t, tt.elevated, grant.Grant.Elevated, grant.Grant.Reason)
			assert.Equal(t, tt.elevated, len(application.BootstrapPreview{Changes: changes}.ElevatedGrants()) == 1)
		})
	}
}

func TestVistaPrevia_NadaFueraDeLaSuscripcionDeLaSesion(t *testing.T) {
	// M3: el despliegue es de una suscripción. Otra suscripción, un grupo de administración o la raíz se rechazan.
	other := "/subscriptions/99999999-9999-4999-8999-999999999999/resourceGroups/prod-datos"
	tests := map[string]map[string]any{
		"un rol en otra suscripción":           assignment(other, contributorRole, ownPrincipal),
		"un recurso en otra suscripción":       change("Modify", other+"/providers/Microsoft.Storage/storageAccounts/x", map[string]any{}),
		"un rol en un grupo de administración": assignment("/providers/Microsoft.Management/managementGroups/raiz", "acdd72a7-3385-48ef-bd42-f606fba81ae7", ownPrincipal),
		"otra suscripción que Azure ignora":    change("Ignore", other, nil),
	}
	for name, extra := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := previewOf(encode(t, ownSetup(extra)))

			var rejected *application.BootstrapRejectedError
			require.ErrorAs(t, err, &rejected)
			assert.Equal(t, "V5", rejected.Rule, err.Error())
		})
	}
}

func TestVistaPrevia_UnPermisoSinPrevisualizarQuedaResaltado(t *testing.T) {
	c := assignment(testGroup, contributorRole, "x")
	c["changeType"], c["after"] = "Unsupported", nil
	doc := map[string]any{"status": "Succeeded", "changes": []any{c}}

	changes, err := previewOf(encode(t, doc))

	require.NoError(t, err)
	assert.Equal(t, application.ChangeUnknown, changes[0].Action)
	assert.True(t, changes[0].Grant.Elevated, "lo que no se sabe no se da por inofensivo")
	assert.Len(t, application.BootstrapPreview{Changes: changes}.ElevatedGrants(), 1)
}
