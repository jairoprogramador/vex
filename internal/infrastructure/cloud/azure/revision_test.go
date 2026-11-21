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

// Casos de la revisión de seguridad independiente (testdata/revision, sintéticos): cada hallazgo queda con su
// plantilla o su vista previa de ataque como prueba de regresión.

const reviewSubscription = "00000000-0000-0000-0000-000000000001"

func reviewFile(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "revision", name))
	require.NoError(t, err)
	return raw
}

// withOwnIdentity añade a una plantilla de ataque un grupo y una identidad propios, para que el caso llegue hasta
// la regla que quiere probar y no se quede en «la plantilla no declara ninguna identidad».
func withOwnIdentity(t *testing.T, raw []byte) []byte {
	t.Helper()
	var doc map[string]any
	require.NoError(t, json.Unmarshal(raw, &doc))
	resources, _ := doc["resources"].([]any)
	doc["resources"] = append(resources,
		map[string]any{"type": "Microsoft.Resources/resourceGroups", "name": "rg"},
		map[string]any{
			"type": "Microsoft.Resources/deployments", "name": "identidades", "resourceGroup": "rg",
			"properties": map[string]any{"template": map[string]any{
				"resources": []any{map[string]any{"type": identityType, "name": "id"}},
			}},
		})
	return encode(t, doc)
}

func rejectedRule(t *testing.T, err error) string {
	t.Helper()
	var rejected *application.BootstrapRejectedError
	require.ErrorAs(t, err, &rejected)
	return rejected.Rule
}

func TestRevision_PlantillasQueSeRechazan(t *testing.T) {
	tests := map[string]struct {
		file  string
		rules []string
		own   bool
	}{
		"control: credencial federada anidada":             {"00-control-fic.json", []string{"S1"}, false},
		"M1 credencial federada por la extensión Graph":    {"01-graph-fic-extension.json", []string{"S3"}, false},
		"M2 Owner por solicitud programada (PIM)":          {"02-pim-owner.json", []string{"S7"}, true},
		"A1 identidad de despliegue que ya existía":        {"03-identidad-existente.json", []string{"S6"}, false},
		"M1 tipo construido con concat":                    {"07-tipo-concat.json", []string{"S1", "S4"}, false},
		"M1 tipo con un espacio al final":                  {"07b-tipo-espacio.json", []string{"S1", "S4"}, false},
		"M1 tipo con una barra al final":                   {"07c-tipo-barra.json", []string{"S1", "S4"}, false},
		"M5 credencial federada dentro de una política":    {"08-politica-dine.json", []string{"S1"}, true},
		"M5 contenedor con identidad que ejecuta comandos": {"09-contenedor-codigo.json", []string{"S2"}, true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			raw := reviewFile(t, tt.file)
			if tt.own {
				raw = withOwnIdentity(t, raw)
			}

			_, err := checkTemplate(raw)

			assert.Contains(t, tt.rules, rejectedRule(t, err), "%v", err)
		})
	}
}

func TestRevision_A1_LaIdentidadDevueltaDebeDeclararlaLaPlantilla(t *testing.T) {
	// La plantilla 03 declara una identidad propia pero devuelve otra que ya existe (con Owner): se rechaza sin
	// desplegar, porque la salida apunta a un recurso que la plantilla no declara.
	_, err := checkTemplate(withOwnIdentity(t, reviewFile(t, "03-identidad-existente.json")))

	assert.Equal(t, "S6", rejectedRule(t, err))
	assert.ErrorContains(t, err, "deployIdentityId")

	doc := baseTemplate()
	doc["outputs"].(map[string]any)["pullIdentityId"] = map[string]any{"type": "string",
		"value": "[resourceId('rg-plataforma', '" + identityType + "', 'id-ajena')]"}
	doc["outputs"].(map[string]any)["pullClientId"] = map[string]any{"type": "string", "value": "[x]"}
	_, err = checkTemplate(encode(t, doc))
	assert.Equal(t, "S6", rejectedRule(t, err), "la identidad de descarga sigue la misma regla")

	doc = baseTemplate()
	doc["outputs"].(map[string]any)["deployIdentityId"] = map[string]any{"type": "string",
		"value": "/subscriptions/" + testSubscription + "/resourceGroups/rg-x/providers/" + identityType + "/id-owner"}
	_, err = checkTemplate(encode(t, doc))
	assert.Equal(t, "S6", rejectedRule(t, err), "un identificador escrito a mano no sale de la plantilla")
}

func TestRevision_M1_ElTipoDeUnRecursoEsUnLiteral(t *testing.T) {
	for name, kind := range map[string]string{
		"expresión":     "[concat('Microsoft.Storage/', 'storageAccounts')]",
		"espacio final": "Microsoft.Storage/storageAccounts ",
		"barra final":   "Microsoft.Storage/storageAccounts/",
		"versión":       "Microsoft.Graph/applications@v1.0",
		"sin proveedor": "storageAccounts",
	} {
		t.Run(name, func(t *testing.T) {
			doc := baseTemplate()
			addResource(nested(doc), map[string]any{"type": kind, "name": "x"})

			_, err := checkTemplate(encode(t, doc))

			assert.Equal(t, "S4", rejectedRule(t, err), "%v", err)
		})
	}
	t.Run("extensiones", func(t *testing.T) {
		for _, key := range []string{"imports", "extensions"} {
			doc := baseTemplate()
			doc[key] = map[string]any{"MicrosoftGraph": map[string]any{"provider": "MicrosoftGraph", "version": "1.0.0"}}

			_, err := checkTemplate(encode(t, doc))

			assert.Equal(t, "S3", rejectedRule(t, err), key)
		}
	})
	t.Run("la credencial federada se busca en todos los segmentos", func(t *testing.T) {
		doc := baseTemplate()
		addResource(nested(doc), map[string]any{"type": identityType + "/federatedIdentityCredentials/algo", "name": "id/x/y"})

		_, err := checkTemplate(encode(t, doc))

		assert.Equal(t, "S1", rejectedRule(t, err))
	})
}

func TestRevision_M2_OtrosRecursosDePermisosQuedanResaltados(t *testing.T) {
	tpl, err := checkTemplate(withOwnIdentity(t, reviewFile(t, "10-rol-a-medida.json")))

	require.NoError(t, err)
	require.Len(t, tpl.declared, 1)
	grant := tpl.declared[0]
	assert.True(t, grant.Elevated)
	assert.Contains(t, grant.Role, "definición de rol")
	assert.Contains(t, grant.Reason, "Owner")

	for _, kind := range []string{"policyAssignments", "policyDefinitions", "policySetDefinitions"} {
		doc := baseTemplate()
		addResource(doc, map[string]any{"type": "Microsoft.Authorization/" + kind, "name": "p", "identity": map[string]any{"type": "SystemAssigned"}})
		tpl, err := checkTemplate(encode(t, doc))
		require.NoError(t, err, kind)
		last := tpl.declared[len(tpl.declared)-1]
		assert.True(t, last.Elevated, kind)
		assert.Contains(t, last.Reason, "identidad", "una política con identidad lo dice")
	}

	doc := baseTemplate()
	addResource(nested(doc), map[string]any{"type": "Microsoft.Authorization/locks", "name": "proteger"})
	tpl, err = checkTemplate(encode(t, doc))
	require.NoError(t, err)
	assert.Len(t, tpl.declared, 1, "un bloqueo no es un permiso")

	doc = baseTemplate()
	addResource(doc, map[string]any{"type": "Microsoft.Authorization/roleEligibilityScheduleRequests", "name": "x"})
	_, err = checkTemplate(encode(t, doc))
	assert.Equal(t, "S7", rejectedRule(t, err))
}

func TestRevision_M3_UnDespliegueAnidadoNoEligeOtroDestino(t *testing.T) {
	for _, key := range []string{"subscriptionId", "scope"} {
		doc := baseTemplate()
		doc["resources"].([]any)[1].(map[string]any)[key] = "99999999-9999-4999-8999-999999999999"

		_, err := checkTemplate(encode(t, doc))

		assert.Equal(t, "S5", rejectedRule(t, err), key)
	}
}

func TestRevision_M4_PrincipalYAmbitoAjenosQuedanResaltados(t *testing.T) {
	tpl, err := checkTemplate(withOwnIdentity(t, reviewFile(t, "05-principal-ajeno.json")))

	require.NoError(t, err)
	require.Len(t, tpl.declared, 1)
	grant := tpl.declared[0]
	assert.Equal(t, "Contributor", grant.Role)
	assert.True(t, grant.Elevated, "Contributor solo es conocido sobre lo que la plantilla crea")
	assert.Equal(t, "99999999-9999-9999-9999-999999999999", grant.Identity)
	assert.Equal(t, "grupo de recursos prod-datos", grant.Scope)
	assert.Contains(t, grant.Reason, "identidad que esta plantilla no crea")
	assert.Contains(t, grant.Reason, "grupo de recursos que esta plantilla no crea")
}

func TestRevision_B2_UnaExpresionSeMuestraEntera(t *testing.T) {
	tpl, err := checkTemplate(withOwnIdentity(t, reviewFile(t, "06-etiquetas-falsas.json")))

	require.NoError(t, err)
	require.Len(t, tpl.declared, 1)
	grant := tpl.declared[0]
	assert.True(t, grant.Elevated)
	assert.Contains(t, grant.Identity, "99999999-9999-9999-9999-999999999999", "se ve el valor que de verdad se usará")
	assert.True(t, strings.HasPrefix(grant.Identity, "[if(false,"), "no se resume en el último literal")
	assert.Contains(t, grant.Scope, "prod-secretos")
	assert.Contains(t, grant.Scope, "[if(equals(")
}

func TestRevision_M5_UnRecursoConIdentidadQuedaResaltado(t *testing.T) {
	doc := baseTemplate()
	addResource(nested(doc), map[string]any{
		"type": "Microsoft.DataFactory/factories", "name": "df", "identity": map[string]any{"type": "SystemAssigned"}})

	tpl, err := checkTemplate(encode(t, doc))

	require.NoError(t, err)
	last := tpl.declared[len(tpl.declared)-1]
	assert.True(t, last.Elevated)
	assert.Contains(t, last.Role, "Microsoft.DataFactory/factories")

	doc = baseTemplate()
	addResource(nested(doc), map[string]any{"type": "Microsoft.Storage/storageAccounts", "name": "st", "identity": map[string]any{"type": "None"}})
	tpl, err = checkTemplate(encode(t, doc))
	require.NoError(t, err)
	assert.Len(t, tpl.declared, 1, "sin identidad no hay nada que resaltar")
}

func TestRevision_B3_ValoresPorDefectoQueCambianEnCadaDespliegue(t *testing.T) {
	tests := map[string]struct {
		value    any
		rejected bool
	}{
		"newGuid":                        {"[newGuid()]", true},
		"utcNow sin formato":             {"[utcNow()]", true},
		"utcNow con hora":                {"[utcNow('yyyy-MM-ddTHH:mm')]", true},
		"utcNow con día":                 {"[utcNow('yyyy-MM-dd')]", true},
		"utcNow dentro de otra función":  {"[format('n-{0}', utcNow('u'))]", true},
		"utcNow dentro de un objeto":     {map[string]any{"inicio": []any{"[utcNow()]"}}, true},
		"expresión que no se puede leer": {"[utcNow(]", true},
		"utcNow de año y mes":            {"[utcNow('yyyy-MM-01')]", false},
		"un texto cualquiera":            {"utcNow no es una expresión aquí", false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			doc := baseTemplate()
			doc["parameters"].(map[string]any)["extra"] = map[string]any{"type": "string", "defaultValue": tt.value}

			_, err := checkTemplate(encode(t, doc))

			if !tt.rejected {
				require.NoError(t, err)
				return
			}
			assert.Equal(t, "S6", rejectedRule(t, err))
		})
	}
}

func TestRevision_VistasPreviasQueSeRechazan(t *testing.T) {
	tests := map[string]struct{ file, rule string }{
		"M1 recurso de la extensión Graph, sin identificador": {"w-graph.json", "V4"},
		"M3 otra suscripción":                                 {"w-otra-suscripcion.json", "V5"},
		"M2 solicitud programada (PIM)":                       {"w-pim.json", "V2"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := parseWhatIf(reviewFile(t, tt.file), reviewSubscription)

			assert.Equal(t, tt.rule, rejectedRule(t, err), "%v", err)
		})
	}
}

func TestRevision_M2_ModificarUnaDefinicionDeRolQuedaResaltado(t *testing.T) {
	changes, err := parseWhatIf(reviewFile(t, "w-roldef-modify.json"), reviewSubscription)

	require.NoError(t, err)
	require.Len(t, changes, 1)
	require.NotNil(t, changes[0].Grant)
	assert.True(t, changes[0].Grant.Elevated)
	assert.Len(t, application.BootstrapPreview{Changes: changes}.ElevatedGrants(), 1)
}

func TestRevision_B1_ElMotivoDeAzureSoloLlevaASCII(t *testing.T) {
	changes, err := parseWhatIf(reviewFile(t, "w-unsupported-deploy.json"), reviewSubscription)

	require.NoError(t, err)
	require.Len(t, changes, 1)
	note := changes[0].Note
	for _, r := range note {
		assert.True(t, r >= 0x20 && r <= 0x7e, "carácter fuera de ASCII imprimible: %U", r)
	}
	assert.Contains(t, note, `\u{202e}`, "el cambio de dirección se ve")
	assert.Contains(t, note, `\u{1b}`, "la secuencia ANSI se ve")
	assert.Contains(t, note, `\u{440}evisado`, "un homoglifo cirílico no pasa por una letra latina")
	assert.Contains(t, note, `\u{2714}`)
	assert.Len(t, application.BootstrapPreview{Changes: changes}.Unpreviewed(), 1)
}

func TestRevision_B4_NombresQueEmpiezanPorGuionNoSonIdentidades(t *testing.T) {
	prefix := "/subscriptions/" + testSubscription + "/resourceGroups/"
	for _, id := range []string{
		prefix + "-rg/providers/" + identityType + "/id-deploy",
		prefix + "rg/providers/" + identityType + "/-id-deploy",
		prefix + "rg/providers/" + identityType + "/_id",
	} {
		_, ok := parseIdentity(id)
		assert.False(t, ok, id)
	}
	_, ok := parseIdentity(prefix + "rg-1/providers/" + identityType + "/id-deploy")
	assert.True(t, ok)
}
