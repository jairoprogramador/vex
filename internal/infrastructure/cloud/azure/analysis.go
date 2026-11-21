package azure

import (
	"regexp"
	"strings"

	"github.com/jairoprogramador/vex/internal/application"
)

// Recorrido de la plantilla. Primero se apunta todo lo que la plantilla declara (recursos, grupos, despliegues
// anidados); después se describen los permisos contra ese inventario: un rol solo queda sin resaltar si lo recibe
// una identidad que la plantilla crea, sobre un grupo o un recurso que la plantilla crea.

const (
	typeIdentities  = "microsoft.managedidentity/userassignedidentities"
	typeGroups      = "microsoft.resources/resourcegroups"
	authorizationNS = "microsoft.authorization"
	maxResources    = 2000
	maxFollow       = 16
	opaqueOpen      = "\x01"
	opaqueClose     = "\x02"
	parameterMarker = "p:"
)

var (
	// resourceTypeShape: un tipo escrito tal cual (Espacio.Nombres/tipo/subtipo). Sin expresiones, espacios, barra
	// final ni versión: así lo que vex compara es lo que Azure despliega.
	resourceTypeShape = regexp.MustCompile(`^[A-Za-z0-9.]+(/[A-Za-z0-9]+)+$`)
	childTypeShape    = regexp.MustCompile(`^[A-Za-z0-9]+(/[A-Za-z0-9]+)*$`)
	// coarseDate: un formato de fecha que no cambia entre la vista previa y la aplicación (año y mes).
	coarseDate = regexp.MustCompile(`^[yM0-9 ./_-]+$`)
	// codeRunners son recursos que ejecutan código elegido por la plantilla: con una identidad harían por su cuenta
	// lo que vex no deja hacer a la plantilla. Un bootstrap prepara identidades y permisos; el cómputo lo crea el
	// pipeline al ejecutarse.
	codeRunners = []string{
		"microsoft.containerinstance/containergroups", "microsoft.app/jobs", "microsoft.app/containerapps",
		"microsoft.compute/virtualmachines", "microsoft.compute/virtualmachinescalesets", "microsoft.web/sites",
		"microsoft.automation/automationaccounts", "microsoft.logic/workflows", "microsoft.batch/batchaccounts",
		"microsoft.containerregistry/registries/tasks", "microsoft.containerregistry/registries/taskruns",
		"microsoft.resources/deploymentstacks",
	}
	knownAuthorization = []string{
		"roleassignments", "roledefinitions", "policyassignments", "policydefinitions", "policysetdefinitions",
		"policyexemptions", "roleassignmentschedulerequests", "roleeligibilityschedulerequests", "locks",
	}
)

// tplScope es dónde se evalúan las expresiones de una plantilla y dónde despliega.
type tplScope struct {
	// evalID distingue los parámetros sin valor conocido de plantillas distintas.
	evalID    int
	variables object
	// bound es el valor que el despliegue padre pasa a cada parámetro.
	bound map[string]boundValue
	// group es el grupo de recursos donde despliega, en forma canónica; "" a nivel de suscripción.
	group     string
	groupExpr string
}

type boundValue struct {
	expr  string
	scope *tplScope
}

func opaque(text string) string { return opaqueOpen + text + opaqueClose }

// canon reduce una expresión a un texto comparable: sustituye variables y parámetros enlazados, y despliega
// format() y concat(). Dos expresiones con el mismo texto canónico valen lo mismo; lo que no se puede reducir
// queda como una llamada opaca, marcada con el lugar donde se evalúa si depende de él.
func (s *tplScope) canon(node *exprNode, depth int) string {
	if depth > maxFollow {
		return opaque("?")
	}
	switch node.kind {
	case nodeLiteral, nodeNumber:
		return strings.NewReplacer(opaqueOpen, "", opaqueClose, "").Replace(node.text)
	case nodeRaw:
		return opaque("?" + node.text)
	case nodeProperty:
		return opaque(s.canon(node.args[0], depth+1) + "." + strings.ToLower(node.text))
	case nodeIndex:
		return opaque(s.canon(node.args[0], depth+1) + "[" + s.canon(node.args[1], depth+1) + "]")
	}
	name, _ := node.literalArg(0)
	switch {
	case node.isCall("variables") && len(node.args) == 1:
		if value, ok := s.variables.str(name); ok {
			return s.canon(parseExpression(value), depth+1)
		}
	case node.isCall("parameters") && len(node.args) == 1:
		if value, ok := s.bound[strings.ToLower(name)]; ok {
			return value.scope.canon(parseExpression(value.expr), depth+1)
		}
		if s.evalID == 0 {
			return opaque(parameterMarker + name)
		}
	case node.isCall("format") && len(node.args) > 1:
		if pattern, ok := node.literalArg(0); ok {
			filled, ok := placeholders(pattern, len(node.args)-1, func(i int) string { return s.canon(node.args[i+1], depth+1) })
			if ok {
				return filled
			}
		}
	case node.isCall("concat"):
		var joined strings.Builder
		for _, arg := range node.args {
			joined.WriteString(s.canon(arg, depth+1))
		}
		return joined.String()
	}
	args := make([]string, len(node.args))
	for i, arg := range node.args {
		args[i] = s.canon(arg, depth+1)
	}
	tag := ""
	switch node.text {
	case "parameters":
		tag = "@" + itoa(s.evalID)
	case "resourcegroup", "resourceid", "reference", "deployment", "variables":
		tag = "@" + itoa(s.evalID) + ":" + s.group
	}
	return opaque(node.text + tag + "(" + strings.Join(args, ",") + ")")
}

func itoa(n int) string {
	digits := ""
	for {
		digits = string(rune('0'+n%10)) + digits
		if n /= 10; n == 0 {
			return digits
		}
	}
}

// display convierte un texto canónico en algo legible cuando solo depende de los parámetros que pone vex:
// rg-vex-<projectSlug>-<environment>. Con cualquier otra parte opaca devuelve false.
func display(canon string) (string, bool) {
	var out strings.Builder
	for {
		start := strings.Index(canon, opaqueOpen)
		if start < 0 {
			out.WriteString(canon)
			return out.String(), true
		}
		end := strings.Index(canon, opaqueClose)
		inner := canon[start+1 : max(end, start+1)]
		if end < start || !strings.HasPrefix(inner, parameterMarker) || strings.Contains(inner, opaqueOpen) {
			return "", false
		}
		out.WriteString(canon[:start] + "<" + inner[len(parameterMarker):] + ">")
		canon = canon[end+1:]
	}
}

// exprLabel muestra una expresión que vex no pudo atar a nada de la plantilla: un literal o un parámetro se
// muestran tal cual; cualquier otra cosa, entera (acotada). Nunca se resume: un resumen se puede falsificar.
func exprLabel(expr string) string {
	node := parseExpression(expr)
	switch {
	case expr == "":
		return "(sin determinar)"
	case node.kind == nodeLiteral:
		return clean(node.text, 80)
	case node.isCall("parameters") && len(node.args) == 1 && node.args[0].kind == nodeLiteral:
		return clean(node.args[0].text, 60)
	}
	return clean(expr, 120)
}

type declaredResource struct {
	resource object
	// kind es el tipo completo en minúsculas; name, el nombre canónico (con el del padre delante).
	kind    string
	rawKind string
	name    string
	rawName string
	scope   *tplScope
}

func (r declaredResource) key() string { return r.scope.group + "|" + r.kind + "|" + r.name }

type nestedUnit struct {
	doc   object
	scope *tplScope
}

type analysis struct {
	resources []declaredResource
	// labels nombra cada recurso declarado por su clave.
	labels      map[string]string
	groups      map[string]string
	deployments map[string][]nestedUnit
	declared    []application.PlannedGrant
	lastID      int
}

// analyze recorre la plantilla entera y devuelve los permisos que declara.
func analyze(doc object) (*analysis, error) {
	a := &analysis{labels: map[string]string{}, groups: map[string]string{}, deployments: map[string][]nestedUnit{}}
	root := &tplScope{}
	root.variables, _ = doc.obj("variables")
	if err := a.template(doc, root, 0); err != nil {
		return nil, err
	}
	identities := 0
	for _, r := range a.resources {
		label, readable := display(r.name)
		if !readable {
			label = clean(r.rawName, 80)
		}
		a.labels[r.key()] = clean(label, 80)
		if r.kind == typeGroups && r.scope.group == "" {
			a.groups[r.name] = clean(label, 80)
		}
		if r.kind == typeIdentities {
			identities++
		}
	}
	if identities == 0 {
		return nil, rejected("S6", "la plantilla no declara ninguna identidad administrada "+
			"(Microsoft.ManagedIdentity/userAssignedIdentities): la identidad de despliegue debe crearla la propia plantilla")
	}
	if err := a.identityOutputs(doc, root); err != nil {
		return nil, err
	}
	for _, r := range a.resources {
		grant, isGrant, err := a.describe(r)
		if err != nil {
			return nil, err
		}
		if !isGrant {
			continue
		}
		if len(a.declared) >= maxDeclared {
			return nil, rejected("S4", "la plantilla declara demasiados permisos")
		}
		a.declared = append(a.declared, grant)
	}
	return a, nil
}

func (a *analysis) template(doc object, scope *tplScope, depth int) error {
	if depth > maxNesting {
		return rejected("S4", "la plantilla anida despliegues en exceso")
	}
	parameters, _ := doc.obj("parameters")
	for name, value := range parameters {
		definition, _ := value.(object)
		if fallback, present := definition.get("defaultValue"); present && volatile(fallback, 0) {
			return rejected("S6", "el parámetro %q toma por defecto un valor que cambia en cada despliegue (utcNow, newGuid): "+
				"lo que se aplica no sería lo que se previsualizó. Solo se admite utcNow con un formato de año y mes",
				clean(name, 60))
		}
	}
	resources, err := resourceList(doc)
	if err != nil {
		return err
	}
	for _, resource := range resources {
		if err := a.resource(resource, nil, scope, depth); err != nil {
			return err
		}
	}
	return nil
}

// volatile dice si un valor por defecto usa newGuid() o un utcNow() más fino que el mes.
func volatile(value any, depth int) bool {
	switch node := value.(type) {
	case string:
		return volatileExpression(parseExpression(node))
	case []any:
		for _, item := range node {
			if depth < maxJSONDepth && volatile(item, depth+1) {
				return true
			}
		}
	case object:
		for _, item := range node {
			if depth < maxJSONDepth && volatile(item, depth+1) {
				return true
			}
		}
	}
	return false
}

func volatileExpression(node *exprNode) bool {
	switch {
	case node.kind == nodeRaw:
		lower := strings.ToLower(node.text)
		return strings.Contains(lower, "utcnow") || strings.Contains(lower, "newguid")
	case node.isCall("newguid"):
		return true
	case node.isCall("utcnow"):
		format, ok := node.literalArg(0)
		return !ok || len(node.args) != 1 || !coarseDate.MatchString(format)
	}
	for _, arg := range node.args {
		if volatileExpression(arg) {
			return true
		}
	}
	return false
}

// resourceList devuelve los recursos de una plantilla, que pueden venir como lista o como objeto con nombres
// simbólicos (languageVersion 2.0).
func resourceList(doc object) ([]object, error) {
	value, ok := doc.get("resources")
	if !ok {
		return nil, nil
	}
	var items []any
	switch node := value.(type) {
	case []any:
		items = node
	case object:
		for _, item := range node {
			items = append(items, item)
		}
	default:
		return nil, rejected("S4", "la plantilla tiene una sección de recursos con una forma desconocida")
	}
	resources := make([]object, len(items))
	for i, item := range items {
		resource, ok := item.(object)
		if !ok {
			return nil, rejected("S4", "la plantilla tiene un recurso con una forma desconocida")
		}
		resources[i] = resource
	}
	return resources, nil
}

// forbiddenSegment dice si un tipo nombra una credencial federada (S1) o un script de despliegue (S2) en
// cualquiera de sus segmentos.
func forbiddenSegment(kind string) string {
	for segment := range strings.SplitSeq(strings.ToLower(kind), "/") {
		if segment, _, _ = strings.Cut(segment, "@"); segment == segmentFederated || segment == segmentScripts {
			return segment
		}
	}
	return ""
}

// findForbiddenType busca en todo el documento, a cualquier profundidad (también dentro de la regla de una
// política), una clave `type` que nombre una credencial federada o un script de despliegue.
func findForbiddenType(value any, depth int) error {
	if depth > maxJSONDepth {
		return nil
	}
	switch node := value.(type) {
	case object:
		for key, child := range node {
			if text, isText := child.(string); isText && strings.EqualFold(key, "type") {
				lower := strings.ToLower(text)
				switch {
				case strings.Contains(lower, segmentFederated):
					return rejected("S1", "la plantilla nombra una credencial federada (%s); la confianza la crea vex, nunca la plantilla",
						clean(text, 80))
				case strings.Contains(lower, segmentScripts):
					return rejected("S2", "la plantilla nombra un script de despliegue (%s)", clean(text, 80))
				}
			}
			if err := findForbiddenType(child, depth+1); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range node {
			if err := findForbiddenType(child, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *analysis) resource(resource object, parent *declaredResource, scope *tplScope, depth int) error {
	kind, ok := resource.str("type")
	if !ok || kind == "" {
		return rejected("S4", "la plantilla tiene un recurso sin tipo")
	}
	shape := resourceTypeShape
	if parent != nil {
		shape = childTypeShape
	}
	if !shape.MatchString(kind) {
		return rejected("S4", "el tipo de un recurso debe escribirse tal cual (%s): sin expresiones, espacios, versión ni barra final",
			clean(kind, 80))
	}
	for _, key := range []string{"import", "extension"} {
		if _, present := resource.get(key); present {
			return rejected("S3", "un recurso usa una extensión (%s): solo se admiten recursos de Azure Resource Manager", clean(kind, 80))
		}
	}
	rawName, _ := resource.str("name")
	declared := declaredResource{
		resource: resource, kind: strings.ToLower(kind), rawKind: kind, rawName: rawName, scope: scope,
		name: scope.canon(parseExpression(rawName), 0),
	}
	if parent != nil {
		declared.kind, declared.name = parent.kind+"/"+declared.kind, parent.name+"/"+declared.name
	}
	switch forbiddenSegment(declared.kind) {
	case segmentFederated:
		return rejected("S1", "la plantilla crea una credencial federada (%s); la confianza la crea vex, nunca la plantilla",
			clean(kind, 80))
	case segmentScripts:
		return rejected("S2", "la plantilla ejecuta un script de despliegue (%s)", clean(kind, 80))
	}
	for _, runner := range codeRunners {
		if declared.kind == runner || strings.HasPrefix(declared.kind, runner+"/") {
			return rejected("S2", "la plantilla crea un recurso que ejecuta código (%s): un bootstrap solo prepara identidades y "+
				"permisos; el cómputo lo crea el pipeline al ejecutarse", clean(kind, 80))
		}
	}
	if authorizationKind(declared.kind) != "" && strings.Contains(authorizationKind(declared.kind), "schedule") {
		return rejected("S7", "la plantilla asigna un rol mediante una solicitud programada (%s): los permisos se declaran con "+
			"Microsoft.Authorization/roleAssignments, que es lo que la vista previa sabe mostrar", clean(kind, 80))
	}
	if len(a.resources) >= maxResources {
		return rejected("S4", "la plantilla declara demasiados recursos")
	}
	a.resources = append(a.resources, declared)

	properties, _ := resource.obj("properties")
	if inner, present := properties.get("template"); present {
		nested, ok := inner.(object)
		if !ok || declared.kind != typeDeployments {
			return rejected("S3", "solo un despliegue anidado puede llevar otra plantilla, y debe traerla dentro del archivo")
		}
		child, err := a.nestedScope(resource, properties, nested, scope)
		if err != nil {
			return err
		}
		a.deployments[declared.name] = append(a.deployments[declared.name], nestedUnit{doc: nested, scope: child})
		if err := a.template(nested, child, depth+1); err != nil {
			return err
		}
	} else if declared.kind == typeDeployments {
		return rejected("S3", "un despliegue anidado no trae su plantilla dentro del archivo")
	}

	children, err := resourceList(resource)
	if err != nil {
		return err
	}
	for _, child := range children {
		if err := a.resource(child, &declared, scope, depth); err != nil {
			return err
		}
	}
	return nil
}

// nestedScope es dónde despliega una plantilla anidada y con qué valores se evalúa. Un despliegue anidado que
// apunta a otra suscripción o a otro ámbito se rechaza: el bootstrap despliega en una sola suscripción.
func (a *analysis) nestedScope(deployment, properties, nested object, parent *tplScope) (*tplScope, error) {
	for _, key := range []string{"subscriptionId", "scope"} {
		if _, present := deployment.get(key); present {
			return nil, rejected("S5", "un despliegue anidado elige su propio destino (%s): el bootstrap despliega solo en la "+
				"suscripción de la sesión", key)
		}
	}
	child := &tplScope{group: parent.group, groupExpr: parent.groupExpr}
	if value, present := deployment.get("resourceGroup"); present {
		group, ok := value.(string)
		if !ok || group == "" {
			return nil, rejected("S4", "un despliegue anidado tiene un grupo de recursos con una forma desconocida")
		}
		child.group, child.groupExpr = parent.canon(parseExpression(group), 0), group
	}
	options, _ := properties.obj("expressionEvaluationOptions")
	if mode, _ := options.str("scope"); !strings.EqualFold(mode, "inner") {
		// Sin ámbito propio, las expresiones de la plantilla anidada se evalúan con lo del padre.
		child.evalID, child.variables, child.bound = parent.evalID, parent.variables, parent.bound
		return child, nil
	}
	a.lastID++
	child.evalID = a.lastID
	child.variables, _ = nested.obj("variables")
	child.bound = map[string]boundValue{}
	passed, _ := properties.obj("parameters")
	for name, value := range passed {
		entry, _ := value.(object)
		if expr, ok := entry.str("value"); ok {
			child.bound[strings.ToLower(name)] = boundValue{expr: expr, scope: parent}
		}
	}
	return child, nil
}

// authorizationKind devuelve el tipo de un recurso de Microsoft.Authorization (roleassignments, locks…), también
// cuando se declara a la antigua como «…/providers/roleAssignments». "" si no lo es.
func authorizationKind(kind string) string {
	if rest, ok := strings.CutPrefix(kind, authorizationNS+"/"); ok {
		first, _, _ := strings.Cut(rest, "/")
		return first
	}
	if cut := strings.LastIndex(kind, "/providers/"); cut >= 0 {
		first, _, _ := strings.Cut(kind[cut+len("/providers/"):], "/")
		for _, known := range knownAuthorization {
			if first == known {
				return first
			}
		}
	}
	return ""
}

// follow sigue un valor hasta donde nace: a través de variables, de los parámetros que pasa el despliegue padre
// y de las salidas de los despliegues anidados de la propia plantilla.
func (a *analysis) follow(node *exprNode, scope *tplScope) (*exprNode, *tplScope) {
	for range maxFollow {
		name, _ := node.literalArg(0)
		if node.isCall("variables") && len(node.args) == 1 {
			value, ok := scope.variables.str(name)
			if !ok {
				break
			}
			node = parseExpression(value)
			continue
		}
		if node.isCall("parameters") && len(node.args) == 1 {
			value, ok := scope.bound[strings.ToLower(name)]
			if !ok {
				break
			}
			node, scope = parseExpression(value.expr), value.scope
			continue
		}
		unit, output, ok := a.deploymentOutput(node, scope)
		if !ok {
			break
		}
		outputs, _ := unit.doc.obj("outputs")
		definition, _ := outputs.obj(output)
		value, ok := definition.str("value")
		if !ok {
			break
		}
		node, scope = parseExpression(value), unit.scope
	}
	return node, scope
}

// deploymentOutput reconoce reference(<despliegue anidado>).outputs.<salida>.value cuando el despliegue es uno,
// y solo uno, de los que declara la plantilla.
func (a *analysis) deploymentOutput(node *exprNode, scope *tplScope) (nestedUnit, string, bool) {
	target, names := node.path()
	if len(names) != 3 || !strings.EqualFold(names[0], "outputs") || !strings.EqualFold(names[2], "value") ||
		!target.isCall("reference") || len(target.args) == 0 {
		return nestedUnit{}, "", false
	}
	id, idScope := a.follow(target.args[0], scope)
	if id.kind != nodeCall || len(id.args) < 2 {
		return nestedUnit{}, "", false
	}
	switch id.text {
	case "resourceid", "extensionresourceid", "subscriptionresourceid":
	default:
		return nestedUnit{}, "", false
	}
	kind, _ := id.literalArg(len(id.args) - 2)
	units := a.deployments[idScope.canon(id.args[len(id.args)-1], 0)]
	if strings.ToLower(kind) != typeDeployments || len(units) != 1 {
		return nestedUnit{}, "", false
	}
	return units[0], names[1], true
}

// resourceKey reconoce resourceId('<tipo>', nombres…) y resourceId('<grupo>', '<tipo>', nombres…) y devuelve la
// clave con la que ese recurso estaría declarado.
func (a *analysis) resourceKey(node *exprNode, scope *tplScope) (key, kind, group string, ok bool) {
	node, scope = a.follow(node, scope)
	if !node.isCall("resourceid") {
		return "", "", "", false
	}
	at := 0
	group = scope.group
	if first, _ := node.literalArg(0); !resourceTypeShape.MatchString(first) {
		if len(node.args) < 2 {
			return "", "", "", false
		}
		at, group = 1, scope.canon(node.args[0], 0)
	}
	kind, isLiteral := node.literalArg(at)
	names := node.args[min(at+1, len(node.args)):]
	if !isLiteral || !resourceTypeShape.MatchString(kind) || group == "" || len(names) != strings.Count(kind, "/") {
		return "", "", "", false
	}
	parts := make([]string, len(names))
	for i, name := range names {
		parts[i] = scope.canon(name, 0)
	}
	kind = strings.ToLower(kind)
	return group + "|" + kind + "|" + strings.Join(parts, "/"), kind, group, true
}

func (a *analysis) declares(key string) bool {
	_, ok := a.labels[key]
	return ok
}

// identityOutputs comprueba que las identidades que la plantilla devuelve son recursos que ella misma declara,
// cuando se puede saber sin desplegar. Lo que aquí no se puede determinar lo decide la vista previa.
func (a *analysis) identityOutputs(doc object, root *tplScope) error {
	outputs, _ := doc.obj("outputs")
	for _, name := range []string{"deployIdentityId", "pullIdentityId"} {
		definition, present := outputs.obj(name)
		if !present {
			continue
		}
		expr, _ := definition.str("value")
		node, scope := a.follow(parseExpression(expr), root)
		key, kind, _, resolved := a.resourceKey(node, scope)
		switch {
		case node.kind == nodeLiteral:
			return rejected("S6", "la salida %q es un identificador escrito a mano: debe salir de la identidad que la plantilla declara", name)
		case resolved && (kind != typeIdentities || !a.declares(key)):
			return rejected("S6", "la salida %q apunta a una identidad que la plantilla no declara como recurso: vex solo crea "+
				"su confianza sobre identidades que crea el bootstrap", name)
		}
	}
	return nil
}

// ownPrincipal dice si un principalId es el de una identidad que la plantilla declara, y cómo se llama.
func (a *analysis) ownPrincipal(expr string, scope *tplScope) (string, bool) {
	node, origin := a.follow(parseExpression(expr), scope)
	target, names := node.path()
	if len(names) != 1 || !strings.EqualFold(names[0], "principalId") || !target.isCall("reference") || len(target.args) == 0 {
		return "", false
	}
	key, kind, _, ok := a.resourceKey(target.args[0], origin)
	if !ok || kind != typeIdentities || !a.declares(key) {
		return "", false
	}
	return a.labels[key], true
}

// where nombra el lugar donde despliega un recurso.
func (a *analysis) where(scope *tplScope) (label string, own bool) {
	if scope.group == "" {
		return "la suscripción", true
	}
	if name, ok := a.groups[scope.group]; ok {
		return "grupo de recursos " + name, true
	}
	return "grupo de recursos " + exprLabel(scope.groupExpr), false
}

// describe devuelve el permiso que supone un recurso, si supone alguno: una asignación de rol, otro recurso de
// Microsoft.Authorization o un recurso con identidad propia.
func (a *analysis) describe(r declaredResource) (application.PlannedGrant, bool, error) {
	place, _ := a.where(r.scope)
	identity, _ := r.resource.obj("identity")
	identityKind, hasIdentity := identity.str("type")
	hasIdentity = hasIdentity && !strings.EqualFold(identityKind, "None")

	switch kind := authorizationKind(r.kind); {
	case kind == "roleassignments":
		return a.assignment(r), true, nil
	case kind == "locks":
		return application.PlannedGrant{}, false, nil
	case kind != "":
		grant := application.PlannedGrant{
			Identity: "—", Role: authorizationLabel(kind) + " " + clean(a.labels[r.key()], 60), Scope: place,
			Elevated: true, Reason: authorizationReason(kind),
		}
		if hasIdentity {
			grant.Identity = "identidad propia de la política"
			grant.Reason += "; lleva una identidad con la que actúa por su cuenta"
		}
		return grant, true, nil
	case hasIdentity:
		return application.PlannedGrant{
			Identity: "identidad propia del recurso", Role: "recurso con identidad (" + clean(r.rawKind, 80) + ")", Scope: place,
			Elevated: true, Reason: "el recurso recibe una identidad con la que puede actuar; vex no ve qué ejecuta",
		}, true, nil
	}
	return application.PlannedGrant{}, false, nil
}

func authorizationLabel(kind string) string {
	switch {
	case kind == "roledefinitions":
		return "definición de rol"
	case strings.HasPrefix(kind, "policy"):
		return "política (" + kind + ")"
	}
	return "recurso de permisos (" + kind + ")"
}

func authorizationReason(kind string) string {
	switch {
	case kind == "roledefinitions":
		return "define o cambia un rol: puede dar lo mismo que Owner con otro nombre"
	case strings.HasPrefix(kind, "policy"):
		return "una política puede desplegar recursos y permisos por su cuenta"
	}
	return "vex no sabe qué permisos da este tipo de recurso"
}

// assignment describe una asignación de rol tal como la declara la plantilla. Queda resaltado todo lo que no sea
// un rol conocido, para una identidad que la plantilla crea, sobre algo que la plantilla crea: nunca se da por
// inofensivo lo que no se sabe.
func (a *analysis) assignment(r declaredResource) application.PlannedGrant {
	properties, _ := r.resource.obj("properties")
	grant := application.PlannedGrant{}
	var reasons []string
	flag := func(reason string) {
		if reason != "" {
			grant.Elevated = true
			reasons = append(reasons, reason)
		}
	}

	roleExpr, _ := properties.str("roleDefinitionId")
	ids := uniqueFold(anyGUID.FindAllString(r.scope.canon(parseExpression(roleExpr), 0), -1))
	if len(ids) == 1 {
		var reason string
		grant.Role, _, reason = roleFor(ids[0])
		flag(reason)
	} else {
		grant.Role = "rol sin determinar"
		flag("no se puede saber qué rol es sin desplegar")
	}

	principal, _ := properties.str("principalId")
	if name, own := a.ownPrincipal(principal, r.scope); own {
		grant.Identity = "identidad " + name
	} else {
		grant.Identity = exprLabel(principal)
		flag("la recibe una identidad que esta plantilla no crea")
	}

	place, ownPlace := a.where(r.scope)
	target, hasTarget := r.resource.str("scope")
	switch {
	case hasTarget:
		key, kind, group, ok := a.resourceKey(parseExpression(target), r.scope)
		groupName, ownGroup := a.groups[group]
		switch {
		case ok && ownGroup && a.declares(key):
			grant.Scope = "grupo de recursos " + groupName + " › " + kind[strings.LastIndex(kind, "/")+1:] + " " + a.labels[key]
		case widerThanGroup.MatchString(r.scope.canon(parseExpression(target), 0)):
			grant.Scope = clean(target, 120)
			flag("alcanza más que un grupo de recursos")
		case r.scope.group == "":
			grant.Scope = clean(target, 120)
			flag("no se puede saber qué alcanza sin desplegar")
		default:
			grant.Scope = clean(target, 120)
			flag("su ámbito no es un recurso que cree esta plantilla")
		}
	case r.scope.group == "":
		grant.Scope = "toda la suscripción"
		flag("alcanza toda la suscripción")
	default:
		grant.Scope = place
		if !ownPlace {
			flag("su ámbito es un grupo de recursos que esta plantilla no crea")
		}
	}
	grant.Reason = strings.Join(reasons, "; ")
	return grant
}

func uniqueFold(values []string) []string {
	var out []string
	for _, value := range values {
		value = strings.ToLower(value)
		found := false
		for _, seen := range out {
			found = found || seen == value
		}
		if !found {
			out = append(out, value)
		}
	}
	return out
}
