package azure

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jairoprogramador/vex/internal/application"
)

const (
	// credentialName es la única credencial federada que vex crea y la única que acepta en la identidad.
	credentialName = "vex-oidc"
	// tokenExchangeAudience es la audiencia que Entra exige a un token que se canjea por federación.
	tokenExchangeAudience = "api://AzureADTokenExchange"
)

var (
	// Los nombres de grupo y de identidad empiezan por una letra o un número: nunca por algo que `az` pueda leer
	// como una opción.
	identityParts = regexp.MustCompile(`(?i)^/subscriptions/(` + guid + `)/resourceGroups/([A-Za-z0-9][A-Za-z0-9._()-]{0,89})` +
		`/providers/Microsoft\.ManagedIdentity/userAssignedIdentities/([A-Za-z0-9][A-Za-z0-9_-]{2,127})$`)
	lockNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._()-]{0,89}$`)
	issuerPattern   = regexp.MustCompile(`^https://[a-z0-9.-]{1,200}$`)
	subjectPattern  = regexp.MustCompile(`^vex:p:[0-9a-f-]{36}:e:[a-z0-9][a-z0-9-]{0,30}:c:[0-9a-f-]{36}$`)
)

// identityRef es una identidad administrada descompuesta en lo que `az` necesita.
type identityRef struct {
	subscription string
	group        string
	name         string
}

func parseIdentity(id string) (identityRef, bool) {
	match := identityParts.FindStringSubmatch(id)
	if match == nil {
		return identityRef{}, false
	}
	return identityRef{subscription: strings.ToLower(match[1]), group: match[2], name: match[3]}, true
}

// truster administra la credencial federada de la identidad de despliegue.
type truster struct{ az Runner }

var _ application.TrustProvider = truster{}

func (truster) Audience() string { return tokenExchangeAudience }

// deployIdentity saca de la conexión la identidad de despliegue, que debe vivir en la suscripción de la sesión.
func deployIdentity(account application.CloudAccount, outputs application.ConnectionOutputs) (identityRef, error) {
	subscription, err := subscriptionOf(account)
	if err != nil {
		return identityRef{}, err
	}
	ref, ok := parseIdentity(outputs.Params["deploy_identity_id"])
	if !ok {
		return identityRef{}, errors.New("la conexión no trae una identidad de despliegue válida")
	}
	if ref.subscription != subscription {
		return identityRef{}, errors.New("la identidad de despliegue no está en la suscripción de la sesión de Azure")
	}
	return ref, nil
}

func checkTrust(want application.Trust) error {
	if !issuerPattern.MatchString(want.Issuer) || !subjectPattern.MatchString(want.Subject) || want.Audience != tokenExchangeAudience {
		return errors.New("la confianza que se pide no tiene el emisor, el subject o la audiencia que vex espera")
	}
	return nil
}

// credential es una credencial federada tal como la devuelve `az`.
type credential struct {
	name      string
	issuer    string
	subject   string
	audiences []string
}

func (c credential) matches(want application.Trust) bool {
	return c.issuer == want.Issuer && c.subject == want.Subject && len(c.audiences) == 1 && c.audiences[0] == want.Audience
}

// sameOrigin dice si la credencial es de vex para el mismo proyecto y ambiente que want: todo coincide salvo el
// id de la conexión, que es lo último del subject.
func (c credential) sameOrigin(want application.Trust) bool {
	family := want.Subject[:strings.LastIndex(want.Subject, ":c:")+len(":c:")]
	other := want
	other.Subject = c.subject
	return c.matches(other) && subjectPattern.MatchString(c.subject) && strings.HasPrefix(c.subject, family)
}

func (ref identityRef) args(command ...string) []string {
	return append(command, "--identity-name", ref.name, "--resource-group", ref.group, "--subscription", ref.subscription)
}

func (t truster) list(ctx context.Context, ref identityRef) ([]credential, error) {
	raw, err := t.az.Run(ctx, append(ref.args("identity", "federated-credential", "list"), "--output", "json")...)
	if err != nil {
		return nil, err
	}
	parsed, err := parseStrict(raw)
	if err != nil {
		return nil, fmt.Errorf("la lista de credenciales federadas no se entiende: %w", err)
	}
	items, ok := parsed.([]any)
	if !ok {
		return nil, errors.New("la lista de credenciales federadas no se entiende")
	}
	credentials := make([]credential, 0, len(items))
	for _, item := range items {
		entry, ok := item.(object)
		if !ok {
			return nil, errors.New("la lista de credenciales federadas no se entiende")
		}
		// Según la versión de `az`, los datos vienen en el objeto o dentro de `properties`.
		source := entry
		if properties, nested := entry.obj("properties"); nested {
			source = properties
		}
		c := credential{}
		c.name, _ = entry.str("name")
		c.issuer, _ = source.str("issuer")
		c.subject, _ = source.str("subject")
		audiences, _ := source.get("audiences")
		list, _ := audiences.([]any)
		for _, audience := range list {
			text, _ := audience.(string)
			c.audiences = append(c.audiences, text)
		}
		if c.name == "" {
			return nil, errors.New("la lista de credenciales federadas no se entiende")
		}
		credentials = append(credentials, c)
	}
	return credentials, nil
}

// names lista los nombres de unas credenciales, limpios para mostrarse.
func names(credentials []credential) string {
	out := make([]string, len(credentials))
	for i, c := range credentials {
		out[i] = clean(c.name, 60)
	}
	return strings.Join(out, ", ")
}

func split(credentials []credential) (vex *credential, others []credential) {
	for i, c := range credentials {
		if c.name == credentialName {
			vex = &credentials[i]
		} else {
			others = append(others, c)
		}
	}
	return vex, others
}

// Ensure deja en la identidad de despliegue la confianza want, y solo esa. Una identidad que ya confía en otro
// emisor no se toca: vex no comparte identidad con nadie. Si la credencial que hay es la de otra conexión de este
// mismo proyecto y ambiente (mismo emisor y audiencia; solo cambia el id de la conexión en el subject), se cambia
// por la nueva: es la confianza de vex para este mismo destino, que la conexión nueva sustituye.
func (t truster) Ensure(
	ctx context.Context, account application.CloudAccount, outputs application.ConnectionOutputs, want application.Trust,
) error {
	if err := checkTrust(want); err != nil {
		return err
	}
	ref, err := deployIdentity(account, outputs)
	if err != nil {
		return err
	}
	credentials, err := t.list(ctx, ref)
	if err != nil {
		return err
	}
	current, others := split(credentials)
	if len(others) > 0 {
		return fmt.Errorf("la identidad de despliegue %s ya confía en otros emisores (%s). vex no añade su confianza a "+
			"una identidad compartida: quita esas credenciales federadas o haz que la plantilla cree una identidad propia",
			ref.name, names(others))
	}
	action := "create"
	switch {
	case current == nil:
	case current.matches(want):
		return nil
	case current.sameOrigin(want):
		action = "update"
	default:
		return fmt.Errorf("la identidad de despliegue %s ya tiene una credencial %s que no es la de esta conexión "+
			"(otro emisor, otro proyecto u otra conexión). vex no la cambia. Si ya no la usa nadie, bórrala y repite:\n  %s",
			ref.name, credentialName, commandLine(append(ref.args("identity", "federated-credential", "delete", "--name", credentialName), "--yes")...))
	}
	args := ref.args("identity", "federated-credential", action, "--name", credentialName)
	args = append(args, "--issuer", want.Issuer, "--subject", want.Subject, "--audiences", want.Audience, "--output", "none")
	_, err = t.az.Run(ctx, args...)
	return err
}

// previewedIdentity busca una identidad, por su identificador exacto, entre lo que la vista previa dice que el
// bootstrap crea o mantiene.
func previewedIdentity(preview application.BootstrapPreview, id string) (application.ChangeAction, bool) {
	for _, change := range preview.Changes {
		if !strings.EqualFold(change.ID, id) || strings.ToLower(change.Kind) != typeIdentities {
			continue
		}
		switch change.Action {
		case application.ChangeCreate, application.ChangeModify, application.ChangeNone:
			return change.Action, true
		}
	}
	return "", false
}

// effectiveRole es una asignación de rol que la identidad tiene de verdad.
type effectiveRole struct {
	id    string
	role  string
	scope string
}

// roles lee las asignaciones de rol de un principal, en la suscripción y por encima de ella. Es una lectura con
// la sesión de quien ejecuta; no pasa por Microsoft Graph.
func (t truster) roles(ctx context.Context, subscription, principal string) ([]effectiveRole, error) {
	raw, err := t.az.Run(ctx, "role", "assignment", "list", "--assignee-object-id", principal, "--all",
		"--fill-principal-name", "false", "--fill-role-definition-name", "false", "--subscription", subscription, "--output", "json")
	if err != nil {
		return nil, fmt.Errorf("leer los roles de la identidad de despliegue: %w", err)
	}
	parsed, err := parseStrict(raw)
	items, ok := parsed.([]any)
	if err != nil || !ok {
		return nil, errors.New("la lista de roles de la identidad de despliegue no se entiende")
	}
	found := make([]effectiveRole, 0, len(items))
	for _, item := range items {
		entry, _ := item.(object)
		id, _ := entry.str("id")
		scope, _ := entry.str("scope")
		definition, _ := entry.str("roleDefinitionId")
		owner, _ := entry.str("principalId")
		match := roleDefinition.FindStringSubmatch(definition)
		if match == nil || !strings.EqualFold(owner, principal) || (scope != "/" && !resourceIDShape.MatchString(scope)) {
			return nil, errors.New("la lista de roles de la identidad de despliegue no se entiende")
		}
		found = append(found, effectiveRole{id: id, role: match[1], scope: scope})
	}
	return found, nil
}

// previewedRole dice si la vista previa mostró esta asignación: por su identificador o, cuando Azure no pudo
// calcularlo antes de desplegar, por el mismo rol sobre el mismo ámbito para esta identidad.
func previewedRole(preview application.BootstrapPreview, role effectiveRole, identity, principal string) bool {
	name, _, _ := roleFor(role.role)
	place := scopeLabel(role.scope)
	for _, change := range preview.Changes {
		if change.Grant == nil || authorizationKind(strings.ToLower(change.Kind)) != "roleassignments" {
			continue
		}
		switch change.Action {
		case application.ChangeCreate, application.ChangeModify, application.ChangeNone:
		default:
			continue
		}
		if strings.EqualFold(change.ID, role.id) {
			return true
		}
		who := change.Grant.Identity
		sameIdentity := who == "identidad "+identity || who == "identidad "+principal || strings.HasPrefix(who, "identidad por crear ")
		if sameIdentity && change.Grant.Role == name && change.Grant.Scope == place {
			return true
		}
	}
	return false
}

// Exposure aplica la barandilla de la identidad antes de crear la confianza. Las identidades que devolvió el
// bootstrap tienen que aparecer en la vista previa, con su identificador exacto, como algo que el bootstrap crea o
// mantiene; si no, se rechaza. Después lee los roles que la identidad de despliegue tiene de verdad y devuelve los
// que la vista previa no mostró. Si la identidad ya existía y aún no confiaba en vex, devuelve todos: vex está a
// punto de dar acceso a algo que no nació con este bootstrap. Solo lee.
func (t truster) Exposure(
	ctx context.Context, account application.CloudAccount, preview application.BootstrapPreview,
	outputs application.ConnectionOutputs, want application.Trust,
) ([]application.PlannedGrant, error) {
	if err := checkTrust(want); err != nil {
		return nil, err
	}
	ref, err := deployIdentity(account, outputs)
	if err != nil {
		return nil, err
	}
	created := false
	for _, key := range []string{"deploy_identity_id", "pull_identity_id"} {
		id, present := outputs.Params[key]
		if !present {
			continue
		}
		action, shown := previewedIdentity(preview, id)
		if !shown {
			return nil, rejected("V6", "el bootstrap devolvió una identidad (%s) que la vista previa no mostró como creada por él. "+
				"vex no crea su confianza sobre una identidad que el bootstrap no creó", clean(id, 200))
		}
		if key == "deploy_identity_id" {
			created = action == application.ChangeCreate
		}
	}
	principal := strings.ToLower(outputs.Params["deploy_principal_id"])
	if !uuidLower.MatchString(principal) {
		return nil, errors.New("la conexión no trae un principal de despliegue válido")
	}
	credentials, err := t.list(ctx, ref)
	if err != nil {
		return nil, err
	}
	current, _ := split(credentials)
	trusted := current != nil && (current.matches(want) || current.sameOrigin(want))
	roles, err := t.roles(ctx, ref.subscription, principal)
	if err != nil {
		return nil, err
	}

	var exposed []application.PlannedGrant
	for _, role := range roles {
		shown := previewedRole(preview, role, ref.name, principal)
		if shown && (created || trusted) {
			continue
		}
		grant := application.PlannedGrant{Identity: "identidad " + ref.name, Scope: scopeLabel(role.scope)}
		var reasons []string
		var roleReason string
		grant.Role, grant.Elevated, roleReason = roleFor(role.role)
		if roleReason != "" {
			reasons = append(reasons, roleReason)
		}
		if wide, reason := widerScope(role.scope); wide {
			grant.Elevated = true
			reasons = append(reasons, reason)
		}
		if shown {
			reasons = append(reasons, "la identidad ya existía antes de este bootstrap y vex aún no confiaba en ella")
		} else {
			reasons = append(reasons, "la vista previa no lo mostró")
		}
		grant.Reason = strings.Join(reasons, "; ")
		exposed = append(exposed, grant)
	}
	return exposed, nil
}

// Inspect compara lo que hay con lo que vex dejó. Solo lee.
func (t truster) Inspect(
	ctx context.Context, account application.CloudAccount, outputs application.ConnectionOutputs, want application.Trust,
) ([]application.TrustFinding, error) {
	ref, err := deployIdentity(account, outputs)
	if err != nil {
		return nil, err
	}
	credentials, err := t.list(ctx, ref)
	if err != nil {
		return nil, err
	}
	current, others := split(credentials)
	findings := []application.TrustFinding{{
		OK:   len(others) == 0,
		Text: "La identidad de despliegue " + ref.name + " solo confía en vex",
	}}
	if len(others) > 0 {
		findings[0].Text = fmt.Sprintf("La identidad de despliegue %s confía en emisores que vex no puso (%s): "+
			"quien controle esos emisores puede desplegar en tu suscripción", ref.name, names(others))
	}
	switch {
	case current == nil:
		findings = append(findings, application.TrustFinding{Text: "Falta la credencial federada " + credentialName + " en la identidad de despliegue"})
	case current.matches(want):
		findings = append(findings, application.TrustFinding{OK: true, Text: "La credencial " + credentialName + " tiene el emisor, el subject y la audiencia de esta conexión"})
	default:
		findings = append(findings, application.TrustFinding{Text: "La credencial " + credentialName + " fue modificada: " + differences(*current, want)})
	}
	return findings, nil
}

// differences dice qué no coincide, sin citar valores que no hacen falta.
func differences(c credential, want application.Trust) string {
	var parts []string
	if c.issuer != want.Issuer {
		parts = append(parts, "el emisor es "+clean(c.issuer, 120)+" y no el de vex")
	}
	if c.subject != want.Subject {
		parts = append(parts, "el subject no es el de esta conexión")
	}
	if len(c.audiences) != 1 || c.audiences[0] != want.Audience {
		parts = append(parts, "la audiencia no es "+want.Audience)
	}
	return strings.Join(parts, "; ")
}

// Remove borra la credencial de vex. Si no está, no hay nada que hacer. Si la que hay no es de vex, no se toca.
func (t truster) Remove(
	ctx context.Context, account application.CloudAccount, outputs application.ConnectionOutputs, want application.Trust,
) error {
	ref, err := deployIdentity(account, outputs)
	if err != nil {
		return err
	}
	credentials, err := t.list(ctx, ref)
	if err != nil {
		return err
	}
	current, _ := split(credentials)
	if current == nil {
		return nil
	}
	if current.issuer != want.Issuer {
		return fmt.Errorf("la credencial %s de %s no es de vex (otro emisor); no se borra", credentialName, ref.name)
	}
	remove := append(ref.args("identity", "federated-credential", "delete", "--name", credentialName), "--yes")
	_, err = t.az.Run(ctx, remove...)
	var failed *CommandError
	if errors.As(err, &failed) && strings.Contains(failed.Stderr, "ScopeLocked") {
		return &application.TrustLockedError{Commands: t.unlockCommands(ctx, ref, remove)}
	}
	return err
}

// unlockCommands arma los pasos para retirar la confianza a mano cuando un bloqueo lo impide: quitar el bloqueo,
// borrar la credencial y volver a ponerlo. Solo lee los bloqueos del grupo de la identidad; no quita ninguno.
func (t truster) unlockCommands(ctx context.Context, ref identityRef, remove []string) []string {
	scope := []string{"--resource-group", ref.group, "--subscription", ref.subscription}
	locks := t.groupLocks(ctx, ref)
	if len(locks) == 0 {
		return []string{
			commandLine(append([]string{"lock", "list"}, append(scope, "--output", "table")...)...),
			"# quita el bloqueo que aparezca (az lock delete --name <bloqueo> …), ejecuta el comando siguiente y vuelve a ponerlo",
			commandLine(remove...),
		}
	}
	var commands []string
	for _, lock := range locks {
		commands = append(commands, commandLine(append([]string{"lock", "delete", "--name", lock.name}, scope...)...))
	}
	commands = append(commands, commandLine(remove...))
	for _, lock := range locks {
		commands = append(commands, commandLine(append([]string{"lock", "create", "--name", lock.name, "--lock-type", lock.level}, scope...)...))
	}
	return commands
}

type lock struct{ name, level string }

// groupLocks son los bloqueos puestos sobre el grupo de recursos de la identidad. Si no se pueden leer devuelve
// nil y quien llama da instrucciones genéricas.
func (t truster) groupLocks(ctx context.Context, ref identityRef) []lock {
	raw, err := t.az.Run(ctx, "lock", "list", "--resource-group", ref.group, "--subscription", ref.subscription, "--output", "json")
	if err != nil {
		return nil
	}
	parsed, err := parseStrict(raw)
	items, ok := parsed.([]any)
	if err != nil || !ok {
		return nil
	}
	groupScope := strings.ToLower("/subscriptions/" + ref.subscription + "/resourceGroups/" + ref.group + "/providers/Microsoft.Authorization/locks/")
	var locks []lock
	for _, item := range items {
		entry, _ := item.(object)
		name, _ := entry.str("name")
		level, _ := entry.str("level")
		id, _ := entry.str("id")
		if !lockNamePattern.MatchString(name) || !strings.HasPrefix(strings.ToLower(id), groupScope) {
			continue
		}
		if level != "CanNotDelete" && level != "ReadOnly" {
			continue
		}
		locks = append(locks, lock{name: name, level: level})
	}
	return locks
}

// Cleanup son los comandos para retirar a mano la confianza y para borrar lo que creó el bootstrap. Solo usa
// datos de la conexión que pasan el formato del contrato; con una conexión que no lo cumple no propone nada.
func (truster) Cleanup(outputs application.ConnectionOutputs, operation string) application.CleanupGuide {
	if _, _, err := (Adapter{}).Env(outputs, application.ContainerTokenPath); err != nil {
		return application.CleanupGuide{}
	}
	ref, ok := parseIdentity(outputs.Params["deploy_identity_id"])
	if !ok {
		return application.CleanupGuide{}
	}
	subscription := []string{"--subscription", ref.subscription}
	guide := application.CleanupGuide{
		RemoveTrust: []string{
			commandLine(append(ref.args("identity", "federated-credential", "delete", "--name", credentialName), "--yes")...),
		},
	}
	groups := []string{outputs.Params["resource_group"]}
	if identityGroup := outputs.Params["identity_resource_group"]; identityGroup != "" && identityGroup != groups[0] {
		groups = append(groups, identityGroup)
	}
	if ref.group != groups[0] && (len(groups) == 1 || ref.group != groups[1]) {
		groups = append(groups, ref.group)
	}
	guide.DeleteResources = append(guide.DeleteResources,
		"# roles que tiene la identidad de despliegue (los de fuera de sus grupos de recursos no se borran con ellos):",
		commandLine(append([]string{"role", "assignment", "list", "--assignee", outputs.Params["deploy_principal_id"], "--all", "--output", "table"}, subscription...)...),
		"# para quitar uno: az role assignment delete --ids <id>",
	)
	for _, group := range groups {
		guide.DeleteResources = append(guide.DeleteResources,
			commandLine(append([]string{"group", "delete", "--name", group}, subscription...)...))
	}
	if deploymentPattern.MatchString(operation) {
		guide.DeleteResources = append(guide.DeleteResources,
			commandLine(append([]string{"deployment", "sub", "delete", "--name", operation}, subscription...)...))
	}
	guide.DeleteResources = append(guide.DeleteResources,
		"# si un bloqueo impide borrar un grupo: az lock list --resource-group <grupo> y az lock delete --name <bloqueo> --resource-group <grupo>")
	return guide
}
