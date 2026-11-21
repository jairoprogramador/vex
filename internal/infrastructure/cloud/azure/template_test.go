package azure

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jairoprogramador/vex/internal/application"
)

const subscriptionSchemaURL = "https://schema.management.azure.com/schemas/2018-05-01/subscriptionDeploymentTemplate.json#"

const (
	identityType = "Microsoft.ManagedIdentity/userAssignedIdentities"
	// deployPrincipal es cómo la plantilla base nombra el principal de la identidad que ella misma declara.
	deployPrincipal = "[reference(resourceId('" + identityType + "', format('id-{0}-deploy', parameters('baseName'))), '2023-01-31').principalId]"
	baseIdentity    = "identidad id-vex-<projectSlug>-<environment>-deploy"
	baseGroup       = "grupo de recursos rg-vex-<projectSlug>-<environment>"
)

// baseTemplate es una plantilla mínima que cumple el contrato: un grupo de recursos y, dentro de un despliegue
// anidado, una identidad con un rol sobre ese grupo, como las que compila Bicep.
func baseTemplate() map[string]any {
	stringParam := map[string]any{"type": "string"}
	output := func(expr string) map[string]any { return map[string]any{"type": "string", "value": expr} }
	fromNested := func(name string) map[string]any {
		return output("[reference(extensionResourceId(format('/subscriptions/{0}/resourceGroups/{1}', subscription().subscriptionId, " +
			"format('rg-{0}', variables('baseName'))), 'Microsoft.Resources/deployments', 'vex-bootstrap-carga'), '2025-04-01').outputs." + name + ".value]")
	}
	return map[string]any{
		"$schema":        subscriptionSchemaURL,
		"contentVersion": "1.0.0.0",
		"parameters": map[string]any{
			"projectId": stringParam, "projectSlug": stringParam, "environment": stringParam, "location": stringParam,
			"budgetAmount": map[string]any{"type": "int", "defaultValue": 20},
		},
		"variables": map[string]any{"baseName": "[format('vex-{0}-{1}', parameters('projectSlug'), parameters('environment'))]"},
		"resources": []any{
			map[string]any{"type": "Microsoft.Resources/resourceGroups", "apiVersion": "2024-03-01", "name": "[format('rg-{0}', variables('baseName'))]"},
			map[string]any{
				"type": "Microsoft.Resources/deployments", "apiVersion": "2022-09-01", "name": "vex-bootstrap-carga",
				"resourceGroup": "[format('rg-{0}', variables('baseName'))]",
				"properties": map[string]any{
					"mode":                        "Incremental",
					"expressionEvaluationOptions": map[string]any{"scope": "inner"},
					"parameters":                  map[string]any{"baseName": map[string]any{"value": "[variables('baseName')]"}},
					"template": map[string]any{
						"$schema":    "https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#",
						"parameters": map[string]any{"baseName": stringParam},
						"variables":  map[string]any{"contributor": "b24988ac-6180-42a0-ab88-20f7382dd24c"},
						"resources": []any{
							map[string]any{"type": identityType, "name": "[format('id-{0}-deploy', parameters('baseName'))]"},
							map[string]any{
								"type": "Microsoft.Authorization/roleAssignments", "name": "[guid(resourceGroup().id)]",
								"properties": map[string]any{
									"roleDefinitionId": "[subscriptionResourceId('Microsoft.Authorization/roleDefinitions', variables('contributor'))]",
									"principalId":      deployPrincipal,
								},
							},
						},
						"outputs": map[string]any{
							"deployIdentityId": output("[resourceId('" + identityType + "', format('id-{0}-deploy', parameters('baseName')))]"),
						},
					},
				},
			},
		},
		"outputs": map[string]any{
			"deployIdentityId": fromNested("deployIdentityId"), "deployClientId": output("[x]"), "deployPrincipalId": output("[x]"),
			"resourceGroup": output("[x]"), "registryLoginServer": output("[x]"),
		},
	}
}

// nested devuelve la plantilla anidada de baseTemplate y sus recursos, para manipularlos.
func nested(tpl map[string]any) map[string]any {
	deployment := tpl["resources"].([]any)[1].(map[string]any)
	return deployment["properties"].(map[string]any)["template"].(map[string]any)
}

func addResource(tpl map[string]any, resource map[string]any) {
	tpl["resources"] = append(tpl["resources"].([]any), resource)
}

func roleAssignment(roleExpr string) map[string]any {
	return map[string]any{
		"type": "Microsoft.Authorization/roleAssignments", "name": "[guid('x')]",
		"properties": map[string]any{"roleDefinitionId": roleExpr, "principalId": deployPrincipal},
	}
}

func roleID(id string) string {
	return "[subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '" + id + "')]"
}

func encode(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	return raw
}

func mustEncode(value any) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return raw
}

func TestPlantilla_LaBaseCumpleLasReglas(t *testing.T) {
	tpl, err := checkTemplate(encode(t, baseTemplate()))

	require.NoError(t, err)
	assert.Len(t, tpl.digest, 64)
	assert.Equal(t, []application.PlannedGrant{
		{Identity: baseIdentity, Role: "Contributor", Scope: baseGroup},
	}, tpl.declared)
}

func TestPlantilla_ReglasEstaticas(t *testing.T) {
	federated := map[string]any{"type": "Microsoft.ManagedIdentity/userAssignedIdentities/federatedIdentityCredentials", "name": "id/intruso"}
	tests := map[string]struct {
		rule   string
		change func(tpl map[string]any)
	}{
		"S1 credencial federada en la raíz":               {"S1", func(tpl map[string]any) { addResource(tpl, federated) }},
		"S1 credencial federada en una plantilla anidada": {"S1", func(tpl map[string]any) { addResource(nested(tpl), federated) }},
		"S1 con otras mayúsculas": {"S1", func(tpl map[string]any) {
			addResource(nested(tpl), map[string]any{"TYPE": "MICROSOFT.MANAGEDIDENTITY/USERASSIGNEDIDENTITIES/FEDERATEDIDENTITYCREDENTIALS"})
		}},
		"S1 como recurso hijo de una identidad": {"S1", func(tpl map[string]any) {
			addResource(nested(tpl), map[string]any{
				"type":      "Microsoft.ManagedIdentity/userAssignedIdentities",
				"resources": []any{map[string]any{"type": "federatedIdentityCredentials", "name": "intruso"}},
			})
		}},
		"S1 con recursos por nombre simbólico": {"S1", func(tpl map[string]any) {
			nested(tpl)["resources"] = map[string]any{"fic": federated}
		}},
		"S2 script de despliegue": {"S2", func(tpl map[string]any) {
			addResource(nested(tpl), map[string]any{"type": "Microsoft.Resources/deploymentScripts", "kind": "AzureCLI"})
		}},
		"S3 templateLink": {"S3", func(tpl map[string]any) {
			addResource(tpl, map[string]any{"type": "Microsoft.Resources/deployments", "properties": map[string]any{
				"templateLink": map[string]any{"uri": "https://atacante.test/main.json"}}})
		}},
		"S3 relativePath": {"S3", func(tpl map[string]any) {
			addResource(tpl, map[string]any{"type": "Microsoft.Resources/deployments", "properties": map[string]any{
				"TemplateLink": map[string]any{"RelativePath": "otra.json"}}})
		}},
		"S3 despliegue anidado sin plantilla": {"S3", func(tpl map[string]any) {
			addResource(tpl, map[string]any{"type": "Microsoft.Resources/deployments", "properties": map[string]any{"mode": "Incremental"}})
		}},
		"S3 plantilla anidada que es una expresión": {"S3", func(tpl map[string]any) {
			addResource(tpl, map[string]any{"type": "Microsoft.Resources/deployments", "properties": map[string]any{"template": "[variables('otra')]"}})
		}},
		"S3 plantilla dentro de otro tipo de recurso": {"S1", func(tpl map[string]any) {
			addResource(tpl, map[string]any{"type": "Microsoft.Resources/deploymentStacks", "properties": map[string]any{
				"template": map[string]any{"resources": []any{federated}}}})
		}},
		"S4 recurso sin tipo":              {"S4", func(tpl map[string]any) { addResource(tpl, map[string]any{"name": "x"}) }},
		"S4 recursos que no son una lista": {"S4", func(tpl map[string]any) { tpl["resources"] = "[variables('recursos')]" }},
		"S5 plantilla de grupo de recursos": {"S5", func(tpl map[string]any) {
			tpl["$schema"] = "https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#"
		}},
		"S5 de grupo de administración": {"S5", func(tpl map[string]any) {
			tpl["$schema"] = "https://schema.management.azure.com/schemas/2019-08-01/managementGroupDeploymentTemplate.json#"
		}},
		"S5 sin esquema": {"S5", func(tpl map[string]any) { delete(tpl, "$schema") }},
		"S6 parámetro sin valor por defecto": {"S6", func(tpl map[string]any) {
			tpl["parameters"].(map[string]any)["adminObjectId"] = map[string]any{"type": "string"}
		}},
		"S6 falta un parámetro de vex": {"S6", func(tpl map[string]any) { delete(tpl["parameters"].(map[string]any), "projectSlug") }},
		"S6 parámetro de vex que no es texto": {"S6", func(tpl map[string]any) {
			tpl["parameters"].(map[string]any)["environment"] = map[string]any{"type": "object"}
		}},
		"S6 falta una salida obligatoria": {"S6", func(tpl map[string]any) { delete(tpl["outputs"].(map[string]any), "deployClientId") }},
		"S6 salida segura": {"S6", func(tpl map[string]any) {
			tpl["outputs"].(map[string]any)["clave"] = map[string]any{"type": "secureString", "value": "[x]"}
		}},
		"S6 objeto seguro": {"S6", func(tpl map[string]any) {
			tpl["outputs"].(map[string]any)["claves"] = map[string]any{"type": "SecureObject", "value": "[x]"}
		}},
		"S6 pareja incompleta": {"S6", func(tpl map[string]any) {
			tpl["outputs"].(map[string]any)["stateAccount"] = map[string]any{"type": "string", "value": "[x]"}
		}},
		"S6 demasiadas salidas propias": {"S6", func(tpl map[string]any) {
			for i := range 33 {
				tpl["outputs"].(map[string]any)["extra"+strings.Repeat("x", i)] = map[string]any{"type": "string", "value": "[x]"}
			}
		}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tpl := baseTemplate()
			tt.change(tpl)

			_, err := checkTemplate(encode(t, tpl))

			var rejected *application.BootstrapRejectedError
			require.ErrorAs(t, err, &rejected)
			assert.Equal(t, tt.rule, rejected.Rule, err.Error())
		})
	}
}

func TestPlantilla_JSONQueVexYAzurePodrianLeerDistinto(t *testing.T) {
	valid := string(encode(t, baseTemplate()))
	tests := map[string]string{
		"no es JSON":                          `{"$schema": `,
		"no es un objeto":                     `[1, 2]`,
		"clave repetida":                      strings.Replace(valid, `"contentVersion":"1.0.0.0"`, `"resources":[],"contentVersion":"1.0.0.0"`, 1),
		"clave repetida con otras mayúsculas": strings.Replace(valid, `"contentVersion":"1.0.0.0"`, `"Resources":[],"contentVersion":"1.0.0.0"`, 1),
		"datos después del documento":         valid + ` {"resources": []}`,
		"comentarios":                         "// plantilla\n" + valid,
		"más de 1 MiB":                        strings.Replace(valid, `"contentVersion":"1.0.0.0"`, `"contentVersion":"`+strings.Repeat("1", 1<<20)+`"`, 1),
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := checkTemplate([]byte(raw))

			var rejected *application.BootstrapRejectedError
			require.ErrorAs(t, err, &rejected)
			assert.Equal(t, "S4", rejected.Rule, err.Error())
		})
	}
}

func TestPlantilla_PermisosDeclarados(t *testing.T) {
	const owner, uaa, rbacAdmin = "8e3af657-a8ff-443c-a75c-2fe8c4bcb635", "18d7d88d-d35e-4fb4-a5c3-7773c20a72d9", "f58310d9-a9f6-439a-9e8d-f62e7b41a168"
	tests := map[string]struct {
		change   func(tpl map[string]any)
		role     string
		scope    string
		elevated bool
	}{
		"Owner en un grupo de recursos": {func(tpl map[string]any) { addResource(nested(tpl), roleAssignment(roleID(owner))) },
			"Owner", baseGroup, true},
		"User Access Administrator": {func(tpl map[string]any) { addResource(nested(tpl), roleAssignment(roleID(uaa))) },
			"User Access Administrator", baseGroup, true},
		"Role Based Access Control Administrator, en mayúsculas": {func(tpl map[string]any) {
			addResource(nested(tpl), roleAssignment(roleID(strings.ToUpper(rbacAdmin))))
		}, "Role Based Access Control Administrator", baseGroup, true},
		"Contributor en toda la suscripción": {func(tpl map[string]any) {
			addResource(tpl, roleAssignment(roleID("b24988ac-6180-42a0-ab88-20f7382dd24c")))
		}, "Contributor", "toda la suscripción", true},
		"Reader sobre un recurso del grupo": {func(tpl map[string]any) {
			assignment := roleAssignment(roleID("acdd72a7-3385-48ef-bd42-f606fba81ae7"))
			assignment["scope"] = "[resourceId('Microsoft.Storage/storageAccounts', variables('cuenta'))]"
			addResource(nested(tpl), map[string]any{"type": "Microsoft.Storage/storageAccounts", "name": "[variables('cuenta')]"})
			addResource(nested(tpl), assignment)
		}, "Reader", baseGroup + " › storageaccounts [variables('cuenta')]", false},
		"Reader sobre un recurso que la plantilla no crea": {func(tpl map[string]any) {
			assignment := roleAssignment(roleID("acdd72a7-3385-48ef-bd42-f606fba81ae7"))
			assignment["scope"] = "[resourceId('Microsoft.Storage/storageAccounts', 'cuentaajena')]"
			addResource(nested(tpl), assignment)
		}, "Reader", "[resourceId('Microsoft.Storage/storageAccounts', 'cuentaajena')]", true},
		"ámbito de grupo de administración": {func(tpl map[string]any) {
			assignment := roleAssignment(roleID("acdd72a7-3385-48ef-bd42-f606fba81ae7"))
			assignment["scope"] = "[tenantResourceId('Microsoft.Management/managementGroups', 'raiz')]"
			addResource(nested(tpl), assignment)
		}, "Reader", "[tenantResourceId('Microsoft.Management/managementGroups', 'raiz')]", true},
		"ámbito que no se puede determinar": {func(tpl map[string]any) {
			assignment := roleAssignment(roleID("acdd72a7-3385-48ef-bd42-f606fba81ae7"))
			assignment["scope"] = "[parameters('donde')]"
			addResource(nested(tpl), assignment)
		}, "Reader", "[parameters('donde')]", true},
		"ámbito de suscripción escondido en una variable": {func(tpl map[string]any) {
			nested(tpl)["variables"].(map[string]any)["donde"] = "[subscription().id]"
			assignment := roleAssignment(roleID("acdd72a7-3385-48ef-bd42-f606fba81ae7"))
			assignment["scope"] = "[variables('donde')]"
			addResource(nested(tpl), assignment)
		}, "Reader", "[variables('donde')]", true},
		"rol que vex no reconoce": {func(tpl map[string]any) {
			addResource(nested(tpl), roleAssignment(roleID("11111111-2222-4333-8444-555555555555")))
		}, "rol 11111111-2222-4333-8444-555555555555", baseGroup, true},
		"rol que llega por parámetro": {func(tpl map[string]any) { addResource(nested(tpl), roleAssignment("[parameters('rol')]")) },
			"rol sin determinar", baseGroup, true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			doc := baseTemplate()
			tt.change(doc)

			tpl, err := checkTemplate(encode(t, doc))

			require.NoError(t, err)
			grant := tpl.declared[len(tpl.declared)-1]
			assert.Equal(t, tt.role, grant.Role)
			assert.Equal(t, tt.scope, grant.Scope)
			assert.Equal(t, tt.elevated, grant.Elevated, grant.Reason)
			assert.Equal(t, tt.elevated, grant.Reason != "")
		})
	}
}

func TestPlantilla_UnNombreHostilNoFalsificaLaVistaPrevia(t *testing.T) {
	doc := baseTemplate()
	assignment := roleAssignment(roleID("8e3af657-a8ff-443c-a75c-2fe8c4bcb635"))
	assignment["properties"].(map[string]any)["principalId"] = "[parameters('x\u001b[2K\rlector‮')]"
	addResource(nested(doc), assignment)

	tpl, err := checkTemplate(encode(t, doc))

	require.NoError(t, err)
	grant := tpl.declared[len(tpl.declared)-1]
	assert.NotContains(t, grant.Identity, "\x1b")
	assert.NotContains(t, grant.Identity, "\r")
	assert.NotContains(t, grant.Identity, "‮")
	assert.True(t, grant.Elevated)
}

func TestPlantilla_SeValidanLosBytesQueSeHashearon(t *testing.T) {
	// B5: el adaptador no vuelve a leer el disco. Aunque la carpeta ya no tenga la plantilla (o tenga otra), lo
	// que se comprueba y se aplica son los bytes que el lector entregó junto con la huella.
	bootstrap := application.PipelineBootstrap{
		Dir: t.TempDir(), Manifest: application.BootstrapManifest{Template: "main.json"}, Template: encode(t, baseTemplate()),
	}
	require.NoError(t, os.WriteFile(filepath.Join(bootstrap.Dir, "main.json"), []byte(`{"otra": "plantilla"}`), 0o600))

	tpl, err := loadTemplate(bootstrap)

	require.NoError(t, err)
	assert.Equal(t, string(bootstrap.Template), string(tpl.raw))

	bootstrap.Template = nil
	_, err = loadTemplate(bootstrap)
	var rejected *application.BootstrapRejectedError
	require.ErrorAs(t, err, &rejected)
}

// Las plantillas reales de los pipelines de vex pasan las reglas. Se lee de VEX_PIPELINES_DIR (la carpeta que
// contiene los repos de plantillas); sin ella la prueba se salta, porque esos repos no forman parte de este.
func TestPlantilla_LasPlantillasRealesPasanLasReglas(t *testing.T) {
	root := os.Getenv("VEX_PIPELINES_DIR")
	if root == "" {
		t.Skip("define VEX_PIPELINES_DIR para comprobar las plantillas reales")
	}
	paths, err := filepath.Glob(filepath.Join(root, "*", "bootstrap", "azure", "main.json"))
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	for _, path := range paths {
		t.Run(filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(path)))), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			require.NoError(t, err)

			tpl, err := checkTemplate(raw)

			require.NoError(t, err)
			require.NotEmpty(t, tpl.declared)
			for _, grant := range tpl.declared {
				t.Logf("%s → %s → %s (resaltado: %v %s)", grant.Identity, grant.Role, grant.Scope, grant.Elevated, grant.Reason)
				assert.False(t, grant.Elevated, "las plantillas de vex no piden permisos resaltados")
			}
		})
	}
}
