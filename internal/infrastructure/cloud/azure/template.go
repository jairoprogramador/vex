package azure

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/jairoprogramador/vex/internal/application"
)

// Reglas estáticas sobre la plantilla del pipeline. vex no decide qué permisos se configuran, pero sí impone
// que la plantilla no cree confianzas (S1), no ejecute código (S2), no traiga nada de fuera (S3), sea un JSON
// acotado e inequívoco (S4), sea un despliegue de suscripción (S5) y cumpla el contrato de parámetros y salidas
// (S6). Recorren el documento entero, también las plantillas anidadas.

const (
	maxTemplateBytes = 1 << 20
	maxNesting       = 8
	maxDeclared      = 200

	typeDeployments     = "microsoft.resources/deployments"
	typeRoleAssignments = "microsoft.authorization/roleassignments"
	segmentFederated    = "federatedidentitycredentials"
	segmentScripts      = "deploymentscripts"
)

var subscriptionSchema = regexp.MustCompile(
	`(?i)^https?://schema\.management\.azure\.com/schemas/[0-9]{4}-[0-9]{2}-[0-9]{2}/subscriptionDeploymentTemplate\.json#?$`)

// Lo que vex le pasa a la plantilla y lo que la plantilla le devuelve (contrato azure/v1).
var (
	vexParameters   = []string{"projectId", "projectSlug", "environment", "location"}
	requiredOutputs = []string{"deployIdentityId", "deployClientId", "deployPrincipalId", "resourceGroup"}
	// outputParams relaciona cada salida del contrato con el dato de la conexión.
	outputParams = map[string]string{
		"deployIdentityId":      "deploy_identity_id",
		"deployClientId":        "deploy_client_id",
		"deployPrincipalId":     "deploy_principal_id",
		"resourceGroup":         "resource_group",
		"identityResourceGroup": "identity_resource_group",
		"pullIdentityId":        "pull_identity_id",
		"pullClientId":          "pull_client_id",
		"stateAccount":          "state_account",
		"stateContainer":        "state_container",
	}
	outputPairs = [][2]string{{"pullIdentityId", "pullClientId"}, {"stateAccount", "stateContainer"}}
)

// template es la plantilla del pipeline ya comprobada.
type template struct {
	raw []byte
	// sha256 de raw: lo que se previsualiza y lo que se aplica deben ser los mismos bytes.
	digest string
	// declared son los permisos que la plantilla declara, leídos de sus recursos.
	declared []application.PlannedGrant
}

func rejected(rule, format string, args ...any) error {
	return &application.BootstrapRejectedError{Rule: rule, Detail: fmt.Sprintf(format, args...)}
}

// loadTemplate aplica las reglas estáticas a la plantilla del bootstrap. Usa los bytes que se leyeron al calcular
// la huella: lo que se valida, se previsualiza y se aplica es exactamente lo que se hasheó, no lo que haya en la
// carpeta del pipeline un instante después.
func loadTemplate(bootstrap application.PipelineBootstrap) (*template, error) {
	if len(bootstrap.Template) == 0 {
		return nil, rejected("S3", "el bootstrap no trae su plantilla: debe ser un archivo de la carpeta del bootstrap")
	}
	return checkTemplate(bootstrap.Template)
}

// checkTemplate aplica las reglas S1–S6 a los bytes de una plantilla.
func checkTemplate(raw []byte) (*template, error) {
	if len(raw) > maxTemplateBytes {
		return nil, rejected("S4", "la plantilla pesa más de 1 MiB")
	}
	doc, err := parseObject(raw)
	if err != nil {
		return nil, rejected("S4", "%v", err)
	}
	if schema, _ := doc.str("$schema"); !subscriptionSchema.MatchString(schema) {
		return nil, rejected("S5", "la plantilla no es un despliegue de suscripción ($schema)")
	}
	for _, key := range []string{"imports", "extensions"} {
		if _, present := doc.get(key); present {
			return nil, rejected("S3", "la plantilla usa extensiones (%s): solo se admiten recursos de Azure Resource Manager", key)
		}
	}
	if key := findRemoteReference(doc, 0); key != "" {
		return nil, rejected("S3", "la plantilla trae contenido de fuera (%s); todo debe estar en el archivo", key)
	}
	if kind := findForbiddenType(doc, 0); kind != nil {
		return nil, kind
	}
	if err := checkContract(doc); err != nil {
		return nil, err
	}
	found, err := analyze(doc)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	return &template{raw: raw, digest: hex.EncodeToString(sum[:]), declared: found.declared}, nil
}

// findRemoteReference busca en todo el documento las claves con las que una plantilla enlaza a otra que no está
// en el archivo. Devuelve la primera que encuentra.
func findRemoteReference(value any, depth int) string {
	if depth > maxJSONDepth {
		return ""
	}
	switch node := value.(type) {
	case object:
		for key, child := range node {
			switch strings.ToLower(key) {
			case "templatelink", "parameterslink", "relativepath":
				return clean(key, 40)
			}
			if found := findRemoteReference(child, depth+1); found != "" {
				return found
			}
		}
	case []any:
		for _, child := range node {
			if found := findRemoteReference(child, depth+1); found != "" {
				return found
			}
		}
	}
	return ""
}

// checkContract es S6: los cuatro parámetros de vex están declarados, cualquier otro tiene valor por defecto, las
// salidas obligatorias existen, ninguna salida es segura y las opcionales vienen en pareja.
func checkContract(doc object) error {
	parameters, _ := doc.obj("parameters")
	for _, name := range vexParameters {
		definition, ok := parameters[name].(object)
		if !ok {
			return rejected("S6", "la plantilla no declara el parámetro %q", name)
		}
		if kind, _ := definition.str("type"); !strings.EqualFold(kind, "string") {
			return rejected("S6", "el parámetro %q debe ser de tipo string", name)
		}
	}
	for name, value := range parameters {
		definition, ok := value.(object)
		if !ok {
			return rejected("S6", "el parámetro %q no tiene una definición válida", clean(name, 60))
		}
		if _, hasDefault := definition.get("defaultValue"); !hasDefault && !slices.Contains(vexParameters, name) {
			return rejected("S6", "el parámetro %q no tiene valor por defecto y vex no lo conoce", clean(name, 60))
		}
	}

	outputs, _ := doc.obj("outputs")
	extras := 0
	for name, value := range outputs {
		definition, ok := value.(object)
		if !ok {
			return rejected("S6", "la salida %q no tiene una definición válida", clean(name, 60))
		}
		kind, _ := definition.str("type")
		if strings.HasPrefix(strings.ToLower(kind), "secure") {
			return rejected("S6", "la salida %q es segura: una conexión no guarda secretos", clean(name, 60))
		}
		if _, known := outputParams[name]; known && !strings.EqualFold(kind, "string") {
			return rejected("S6", "la salida %q debe ser de tipo string", name)
		}
		if _, known := outputParams[name]; !known && strings.EqualFold(kind, "string") {
			extras++
		}
	}
	for _, name := range requiredOutputs {
		if _, ok := outputs[name]; !ok {
			return rejected("S6", "la plantilla no devuelve la salida obligatoria %q", name)
		}
	}
	for _, pair := range outputPairs {
		_, first := outputs[pair[0]]
		if _, second := outputs[pair[1]]; first != second {
			return rejected("S6", "las salidas %q y %q van juntas o no van", pair[0], pair[1])
		}
	}
	if extras > maxExtras {
		return rejected("S6", "la plantilla devuelve más de %d salidas propias", maxExtras)
	}
	return nil
}

// Roles integrados que vex sabe nombrar. Los tres primeros dan control sobre los permisos.
var (
	elevatedRoles = map[string]string{
		"8e3af657-a8ff-443c-a75c-2fe8c4bcb635": "Owner",
		"18d7d88d-d35e-4fb4-a5c3-7773c20a72d9": "User Access Administrator",
		"f58310d9-a9f6-439a-9e8d-f62e7b41a168": "Role Based Access Control Administrator",
	}
	knownRoles = map[string]string{
		"b24988ac-6180-42a0-ab88-20f7382dd24c": "Contributor",
		"acdd72a7-3385-48ef-bd42-f606fba81ae7": "Reader",
		"7f951dda-4ed3-4680-a7ca-43fe172d538d": "AcrPull",
		"8311e382-0749-4cb8-b61a-304f252e45ec": "AcrPush",
		"f1a07417-d97a-45cb-824c-7a7467783830": "Managed Identity Operator",
		"ba92f5b4-2d11-453d-a403-e96b0029c9fe": "Storage Blob Data Contributor",
		"2a2b9908-6ea1-4ae2-8e65-a410df84e7d1": "Storage Blob Data Reader",
		"b1ff04bb-8a4e-4dc4-8eb5-8693973ce19b": "Azure Kubernetes Service RBAC Cluster Admin",
		"0ab0b1a8-8aac-4efd-b8c2-3ee1fb270be8": "Azure Kubernetes Service Cluster Admin Role",
		"4abbcc35-e782-43d8-92c5-2d3f1bd2253f": "Azure Kubernetes Service Cluster User Role",
		"4d97b98b-1d4f-4787-a291-c67834d212e7": "Network Contributor",
		"4633458b-17de-408a-b874-0445c86b69e6": "Key Vault Secrets User",
	}
	anyGUID        = regexp.MustCompile(`(?i)` + guid)
	widerThanGroup = regexp.MustCompile(`(?i)managementgroups|tenant\s*\(|subscription\s*\(|^\s*\[?\s*'?/'?\s*\]?\s*$`)
)

// roleFor nombra un rol por su id y dice si exige confirmación escrita: los que controlan permisos y los que vex
// no reconoce (un rol a medida puede dar lo mismo que Owner).
func roleFor(id string) (name string, elevated bool, reason string) {
	id = strings.ToLower(id)
	if name, ok := elevatedRoles[id]; ok {
		return name, true, "da control sobre los permisos"
	}
	if name, ok := knownRoles[id]; ok {
		return name, false, ""
	}
	return "rol " + id, true, "vex no reconoce este rol"
}
