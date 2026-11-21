package azure

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/jairoprogramador/vex/internal/application"
)

// Reglas sobre la vista previa (what-if). vex la muestra; solo impone que no borre nada (V1), que no cree
// confianzas ni ejecute código (V2), que los permisos se listen con los de alto privilegio resaltados (V3) y que
// una respuesta con una forma que no conoce no se aplique (V4).

const maxWhatIfChanges = 500

var (
	whatIfKeys = []string{"changes", "potentialChanges", "diagnostics", "error", "status"}
	changeKeys = []string{
		"after", "before", "changeType", "delta", "resourceId", "unsupportedReason",
		"deploymentId", "identifiers", "symbolicName", "extension",
	}
	// resourceIDShape: una ruta de Azure, sin nada que pueda confundir a quien la lee.
	resourceIDShape = regexp.MustCompile(`^(/[A-Za-z0-9][A-Za-z0-9._()~@+ -]{0,259})+$`)
	subscriptionID  = regexp.MustCompile(`(?i)^/subscriptions/` + guid + `$`)
	roleDefinition  = regexp.MustCompile(`(?i)/providers/Microsoft\.Authorization/roleDefinitions/(` + guid + `)$`)
)

func unknownShape(format string, args ...any) error {
	return rejected("V4", "la vista previa de Azure tiene una forma que vex no conoce (%s); no se aplica nada. "+
		"Actualiza vex", fmt.Sprintf(format, args...))
}

// previewed es un cambio de la vista previa con lo que hace falta para juzgar sus permisos.
type previewed struct {
	change   application.PlannedChange
	resource resourceRef
	before   object
	after    object
}

// parseWhatIf convierte la respuesta de `az deployment sub what-if` en cambios neutrales y le aplica las reglas V.
// subscription es la de la sesión: todo lo que la vista previa toque tiene que estar en ella.
func parseWhatIf(raw []byte, subscription string) ([]application.PlannedChange, error) {
	doc, err := parseObject(raw)
	if err != nil {
		return nil, unknownShape("%v", err)
	}
	for key := range doc {
		if !slices.Contains(whatIfKeys, key) {
			return nil, unknownShape("campo %q", clean(key, 40))
		}
	}
	if status, _ := doc.str("status"); status != "Succeeded" {
		return nil, unknownShape("estado %q", clean(status, 40))
	}
	if failure, present := doc["error"]; present && failure != nil {
		return nil, unknownShape("la vista previa trae un error")
	}
	listed, ok := doc["changes"].([]any)
	if !ok {
		return nil, unknownShape("sin lista de cambios")
	}
	// Los cambios «posibles» son recursos cuyo efecto Azure no asegura: se someten a las mismas reglas.
	if potential, present := doc["potentialChanges"]; present && potential != nil {
		more, ok := potential.([]any)
		if !ok {
			return nil, unknownShape("cambios posibles con otra forma")
		}
		listed = append(slices.Clone(listed), more...)
	}
	if len(listed) > maxWhatIfChanges {
		return nil, unknownShape("más de %d cambios", maxWhatIfChanges)
	}

	entries := make([]previewed, 0, len(listed))
	for _, item := range listed {
		entry, ok := item.(object)
		if !ok {
			return nil, unknownShape("un cambio que no es un objeto")
		}
		parsed, err := plannedChange(entry, subscription)
		if err != nil {
			return nil, err
		}
		entries = append(entries, parsed)
	}
	own := ownership(entries)
	changes := make([]application.PlannedChange, 0, len(entries))
	for _, entry := range entries {
		change := entry.change
		grant, err := own.grant(entry)
		if err != nil {
			return nil, err
		}
		if grant != nil {
			change.Grant = grant
			change.Name = grant.Identity + " → " + grant.Role + " → " + grant.Scope
		}
		changes = append(changes, change)
	}
	slices.SortStableFunc(changes, func(a, b application.PlannedChange) int {
		return strings.Compare(a.Kind+"\x00"+a.Name, b.Kind+"\x00"+b.Name)
	})
	return changes, nil
}

func plannedChange(entry object, subscription string) (previewed, error) {
	none := previewed{}
	for key := range entry {
		if !slices.Contains(changeKeys, key) {
			return none, unknownShape("campo %q en un cambio", clean(key, 40))
		}
	}
	id, _ := entry["resourceId"].(string)
	resource, ok := splitResourceID(id)
	if !ok {
		return none, unknownShape("un identificador de recurso que no se entiende")
	}
	// El despliegue es de una sola suscripción: nada de lo que toque puede estar en otra ni por encima de ella.
	if prefix := "/subscriptions/" + subscription; !strings.EqualFold(id, prefix) && !strings.HasPrefix(strings.ToLower(id), prefix+"/") {
		return none, rejected("V5", "la vista previa toca algo fuera de la suscripción de la sesión (%s); el bootstrap "+
			"despliega solo en ella", clean(id, 200))
	}
	before, err := payload(entry, "before")
	if err != nil {
		return none, err
	}
	after, err := payload(entry, "after")
	if err != nil {
		return none, err
	}
	change := application.PlannedChange{ID: id, Kind: clean(resource.kind, 100), Name: bounded(resource.label, 160)}
	kind, _ := entry["changeType"].(string)
	switch kind {
	case "Create":
		change.Action = application.ChangeCreate
	case "Modify":
		change.Action = application.ChangeModify
	case "NoChange":
		change.Action = application.ChangeNone
	case "Ignore":
		change.Action = application.ChangeIgnored
	case "Unsupported", "Deploy":
		change.Action = application.ChangeUnknown
		reason, _ := entry["unsupportedReason"].(string)
		change.Note = clean(reason, 200)
		if change.Note == "" {
			change.Note = "Azure no dice qué cambiaría"
		}
	case "Delete":
		return none, rejected("V1", "la vista previa borraría %s; vex nunca aplica un bootstrap que borra", clean(id, 160))
	default:
		return none, unknownShape("tipo de cambio %q", clean(kind, 40))
	}
	// Lo que Azure ignora ya existe y la plantilla no lo toca (ahí está la confianza que creó vex): no cuenta.
	// El tipo sale del identificador y, si el recurso viene descrito, también de su descripción: basta con que
	// uno de los dos sea una confianza, un script o algo que ejecuta código para rechazar.
	var kinds []string
	if change.Action != application.ChangeIgnored {
		kinds = append(kinds, resource.kind)
		for _, described := range []object{before, after} {
			if kind, ok := described.str("type"); ok {
				kinds = append(kinds, kind)
			}
		}
	}
	for _, kind := range kinds {
		lower := strings.ToLower(kind)
		switch forbiddenSegment(lower) {
		case segmentFederated:
			return none, rejected("V2", "la vista previa incluye una credencial federada (%s); la confianza la crea vex, nunca la plantilla",
				clean(id, 160))
		case segmentScripts:
			return none, rejected("V2", "la vista previa incluye un script de despliegue (%s)", clean(id, 160))
		}
		for _, runner := range codeRunners {
			if lower == runner || strings.HasPrefix(lower, runner+"/") {
				return none, rejected("V2", "la vista previa incluye un recurso que ejecuta código (%s)", clean(id, 160))
			}
		}
		if strings.Contains(authorizationKind(lower), "schedule") {
			return none, rejected("V2", "la vista previa asigna un rol mediante una solicitud programada (%s)", clean(id, 160))
		}
	}

	if (change.Action == application.ChangeCreate || change.Action == application.ChangeModify) && after == nil {
		return none, unknownShape("un recurso que se crea o cambia sin descripción")
	}
	return previewed{change: change, resource: resource, before: before, after: after}, nil
}

// owned es lo que la vista previa dice que este bootstrap crea: sobre eso, y solo sobre eso, un rol conocido no
// se resalta.
type owned struct {
	// created son los identificadores (en minúsculas) de lo que se crea en este despliegue.
	created map[string]bool
	// principals relaciona el principalId de cada identidad de la plantilla con su nombre.
	principals map[string]string
	// identities son los nombres de las identidades que el bootstrap crea o mantiene.
	identities map[string]bool
}

func ownership(entries []previewed) owned {
	own := owned{created: map[string]bool{}, principals: map[string]string{}, identities: map[string]bool{}}
	for _, entry := range entries {
		switch entry.change.Action {
		case application.ChangeCreate:
			own.created[strings.ToLower(entry.change.ID)] = true
		case application.ChangeModify, application.ChangeNone:
		default:
			continue
		}
		if strings.ToLower(entry.resource.kind) != typeIdentities {
			continue
		}
		own.identities[entry.resource.name] = true
		for _, described := range []object{entry.before, entry.after} {
			properties, _ := described.obj("properties")
			if principal, _ := properties.str("principalId"); guidPattern.MatchString(principal) {
				own.principals[strings.ToLower(principal)] = entry.resource.name
			}
		}
	}
	return own
}

// grant devuelve el permiso que supone un cambio, si supone alguno.
func (own owned) grant(entry previewed) (*application.PlannedGrant, error) {
	if entry.change.Action == application.ChangeIgnored {
		return nil, nil
	}
	kind := authorizationKind(strings.ToLower(entry.resource.kind))
	described := entry.after
	if described == nil {
		described = entry.before
	}
	identity, _ := described.obj("identity")
	identityKind, hasIdentity := identity.str("type")
	hasIdentity = hasIdentity && !strings.EqualFold(identityKind, "None")
	place := scopeLabel(entry.resource.scope)

	switch {
	case kind == "roleassignments":
		grant, err := own.assignment(entry, described)
		return &grant, err
	case kind == "locks":
		return nil, nil
	case kind != "":
		grant := application.PlannedGrant{
			Identity: "—", Role: authorizationLabel(kind) + " " + clean(entry.resource.name, 60), Scope: place,
			Elevated: true, Reason: authorizationReason(kind),
		}
		if hasIdentity {
			grant.Identity = "identidad propia de la política"
			grant.Reason += "; lleva una identidad con la que actúa por su cuenta"
		}
		return &grant, nil
	case hasIdentity:
		return &application.PlannedGrant{
			Identity: "identidad propia del recurso", Role: "recurso con identidad (" + clean(entry.resource.kind, 80) + ")",
			Scope: place, Elevated: true, Reason: "el recurso recibe una identidad con la que puede actuar; vex no ve qué ejecuta",
		}, nil
	}
	return nil, nil
}

// assignment describe una asignación de rol de la vista previa: identidad → rol → ámbito.
func (own owned) assignment(entry previewed, described object) (application.PlannedGrant, error) {
	resource := entry.resource
	properties, _ := described.obj("properties")
	if properties == nil {
		// Sin descripción (un cambio que Azure no supo previsualizar) solo se conoce el ámbito.
		return application.PlannedGrant{
			Identity: "(sin determinar)", Role: "rol sin determinar", Scope: scopeLabel(resource.scope),
			Elevated: true, Reason: "Azure no dice qué rol es",
		}, nil
	}
	roleID, _ := properties.str("roleDefinitionId")
	match := roleDefinition.FindStringSubmatch(roleID)
	if match == nil {
		return application.PlannedGrant{}, unknownShape("una asignación de rol sin un rol reconocible")
	}
	grant := application.PlannedGrant{Scope: scopeLabel(resource.scope)}
	var reasons []string
	flag := func(reason string) {
		if reason != "" {
			grant.Elevated = true
			reasons = append(reasons, reason)
		}
	}
	var roleReason string
	grant.Role, _, roleReason = roleFor(match[1])
	flag(roleReason)

	principal, _ := properties.str("principalId")
	switch name, known := own.principals[strings.ToLower(principal)]; {
	case known:
		grant.Identity = "identidad " + clean(name, 80)
	case guidPattern.MatchString(principal):
		grant.Identity = "identidad " + strings.ToLower(principal)
		flag("la recibe una identidad que este bootstrap no crea")
	case strings.HasPrefix(principal, "["):
		// Aún no existe. Si la expresión nombra una identidad de este bootstrap se muestra su nombre; si no, la
		// expresión entera. Qué identidad es, y si es de la plantilla, lo dice la lista de permisos declarados.
		grant.Identity = "identidad por crear " + clean(principal, 120)
		if name, ok := pendingIdentity(principal); ok && own.identities[name] {
			grant.Identity = "identidad " + name
		}
	default:
		return application.PlannedGrant{}, unknownShape("una asignación de rol sin identidad reconocible")
	}

	if declared, ok := properties.str("scope"); ok && declared != "" && !strings.EqualFold(declared, resource.scope) {
		return application.PlannedGrant{}, unknownShape("una asignación de rol cuyo ámbito no coincide con su identificador")
	}
	if wide, reason := widerScope(resource.scope); wide {
		flag(reason)
	} else if !own.created[strings.ToLower(resource.scope)] {
		flag("su ámbito ya existía: no lo crea este despliegue")
	}
	grant.Reason = strings.Join(reasons, "; ")
	return grant, nil
}

// payload devuelve la descripción de un recurso (before o after); nil si no viene.
func payload(entry object, key string) (object, error) {
	value, present := entry[key]
	if !present || value == nil {
		return nil, nil
	}
	described, ok := value.(object)
	if !ok {
		return nil, unknownShape("%q no es un objeto", key)
	}
	return described, nil
}

// pendingIdentity reconoce reference(resourceId('…/userAssignedIdentities', '<nombre>'), …).principalId, que es
// como la vista previa deja el principal de una identidad que aún no existe.
func pendingIdentity(expr string) (string, bool) {
	target, names := parseExpression(expr).path()
	if len(names) != 1 || !strings.EqualFold(names[0], "principalId") || !target.isCall("reference") || len(target.args) == 0 {
		return "", false
	}
	id := target.args[0]
	if !id.isCall("resourceid") || len(id.args) < 2 {
		return "", false
	}
	kind, _ := id.literalArg(len(id.args) - 2)
	name, ok := id.literalArg(len(id.args) - 1)
	return name, ok && strings.ToLower(kind) == typeIdentities
}

// widerScope dice si un ámbito alcanza más que un grupo de recursos.
func widerScope(scope string) (bool, string) {
	lower := strings.ToLower(scope)
	switch {
	case scope == "" || scope == "/":
		return true, "alcanza todo el directorio"
	case strings.Contains(lower, "/providers/microsoft.management/managementgroups"):
		return true, "alcanza un grupo de administración"
	case subscriptionID.MatchString(scope):
		return true, "alcanza toda la suscripción"
	case !strings.Contains(lower, "/resourcegroups/"):
		return true, "alcanza más que un grupo de recursos"
	}
	return false, ""
}

// scopeLabel nombra un ámbito sin el identificador de la suscripción, que no aporta nada a quien lee.
func scopeLabel(scope string) string {
	if wide, _ := widerScope(scope); wide && subscriptionID.MatchString(scope) {
		return "toda la suscripción"
	}
	segments := strings.Split(strings.Trim(scope, "/"), "/")
	if len(segments) >= 4 && strings.EqualFold(segments[2], "resourceGroups") {
		label := "grupo de recursos " + segments[3]
		if len(segments) > 4 {
			label += " › " + strings.Join(segments[len(segments)-2:], "/")
		}
		return bounded(label, 160)
	}
	return bounded(scope, 160)
}

// bounded acota un texto que ya se sabe ASCII (sale de un identificador con forma comprobada) y lleva separadores
// propios de vex.
func bounded(text string, limit int) string {
	if runes := []rune(text); len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return text
}

// resourceRef es un identificador de recurso de Azure descompuesto.
type resourceRef struct {
	// kind es el tipo completo (Microsoft.Storage/storageAccounts/blobServices).
	kind string
	// label es cómo se muestra: el grupo de recursos y el nombre.
	label string
	// name es el nombre del recurso, sin el grupo.
	name string
	// scope es sobre qué actúa un recurso de extensión (lo que precede al último /providers/).
	scope string
}

// splitResourceID entiende /subscriptions/<id>[/resourceGroups/<rg>][/providers/<ns>/<tipo>/<nombre>…], también
// con recursos de extensión encadenados. Cualquier otra forma no se acepta.
func splitResourceID(id string) (resourceRef, bool) {
	if len(id) > 2048 || !resourceIDShape.MatchString(id) || strings.Contains(id, "/../") || strings.HasSuffix(id, "/..") {
		return resourceRef{}, false
	}
	segments := strings.Split(strings.TrimPrefix(id, "/"), "/")
	lower := strings.ToLower(id)
	cut := strings.LastIndex(lower, "/providers/")
	if cut < 0 {
		// Sin proveedor solo pueden ser la suscripción o un grupo de recursos.
		switch {
		case len(segments) == 2 && strings.EqualFold(segments[0], "subscriptions"):
			return resourceRef{kind: "Microsoft.Resources/subscriptions", label: "la suscripción", scope: id}, true
		case len(segments) == 4 && strings.EqualFold(segments[0], "subscriptions") && strings.EqualFold(segments[2], "resourceGroups"):
			return resourceRef{kind: "Microsoft.Resources/resourceGroups", label: segments[3], scope: id}, true
		}
		return resourceRef{}, false
	}
	// <ns>/<tipo>/<nombre>[/<subtipo>/<nombre>…]: un espacio de nombres y pares tipo/nombre.
	tail := strings.Split(id[cut+len("/providers/"):], "/")
	if len(tail) < 3 || len(tail)%2 == 0 {
		return resourceRef{}, false
	}
	kind, names := tail[0], []string{}
	for i := 1; i < len(tail); i += 2 {
		kind += "/" + tail[i]
		names = append(names, tail[i+1])
	}
	scope := id[:cut]
	label := strings.Join(names, "/")
	if group := resourceGroupOf(scope); group != "" {
		label = group + " › " + label
	}
	return resourceRef{kind: kind, label: label, name: strings.Join(names, "/"), scope: scope}, true
}

func resourceGroupOf(path string) string {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) >= 4 && strings.EqualFold(segments[2], "resourceGroups") {
		return segments[3]
	}
	return ""
}
