package azure

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jairoprogramador/vex/internal/application"
)

var (
	namespacePattern  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,62}(\.[A-Za-z0-9]{1,63}){1,4}$`)
	slugPattern       = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,19}$`)
	deploymentPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._()-]{0,63}$`)
	uuidLower         = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// bootstrapper aplica el bootstrap del pipeline con la sesión de `az` de quien lo acepta.
type bootstrapper struct{ az Runner }

var _ application.BootstrapProvider = bootstrapper{}

// Account lee la sesión de `az`. Solo salen de ahí identificadores: ningún token.
func (b bootstrapper) Account(ctx context.Context) (application.CloudAccount, error) {
	raw, err := b.az.Run(ctx, "account", "show", "--output", "json")
	var failed *CommandError
	if errors.As(err, &failed) && strings.Contains(strings.ToLower(failed.Stderr), "az login") {
		return application.CloudAccount{}, ErrNoSession
	}
	if err != nil {
		return application.CloudAccount{}, err
	}
	doc, err := parseObject(raw)
	if err != nil {
		return application.CloudAccount{}, fmt.Errorf("`az account show` devolvió algo que no se entiende: %w", err)
	}
	subscription, _ := doc.str("id")
	tenant, _ := doc.str("tenantId")
	if !guidPattern.MatchString(subscription) || !guidPattern.MatchString(tenant) {
		return application.CloudAccount{}, errors.New("`az account show` no devolvió una suscripción y un directorio válidos")
	}
	subscription, tenant = strings.ToLower(subscription), strings.ToLower(tenant)
	user, _ := doc.obj("user")
	userName, _ := user.str("name")
	subscriptionName, _ := doc.str("name")
	tenantName, _ := doc.str("tenantDisplayName")
	return application.CloudAccount{
		Summary: []application.LabeledValue{
			{Label: "Cuenta de Azure", Value: clean(userName, 120)},
			{Label: "Directorio", Value: named(tenantName, tenant)},
			{Label: "Suscripción", Value: named(subscriptionName, subscription)},
		},
		Target: map[string]string{"tenant_id": tenant, "subscription_id": subscription},
	}, nil
}

func named(name, id string) string {
	if name = clean(name, 120); name == "" {
		return id
	}
	return name + " (" + id + ")"
}

// Region es la región pedida o, sin pedirla, la de la conexión que ya existe.
func (bootstrapper) Region(requested string, current *application.ConnectionOutputs) (string, error) {
	if requested == "" && current != nil {
		requested = current.Params["location"]
	}
	if requested == "" {
		return "", errors.New("indica la región de Azure con --location <región> (por ejemplo, --location eastus2)")
	}
	if !regionPattern.MatchString(requested) {
		return "", fmt.Errorf("%q no es una región de Azure: usa su nombre corto, como eastus2", clean(requested, 60))
	}
	return requested, nil
}

func (bootstrapper) ValidateTemplate(bootstrap application.PipelineBootstrap) error {
	_, err := loadTemplate(bootstrap)
	return err
}

func (b bootstrapper) RegisterProviders(ctx context.Context, account application.CloudAccount, namespaces []string) error {
	subscription, err := subscriptionOf(account)
	if err != nil {
		return err
	}
	for _, namespace := range namespaces {
		if !namespacePattern.MatchString(namespace) {
			return fmt.Errorf("el manifiesto pide registrar %q, que no es un espacio de nombres de Azure", clean(namespace, 80))
		}
	}
	for _, namespace := range namespaces {
		if _, err := b.az.Run(ctx, "provider", "register", "--namespace", namespace, "--wait",
			"--subscription", subscription, "--output", "none"); err != nil {
			return fmt.Errorf("registrar %s: %w", namespace, err)
		}
	}
	return nil
}

func subscriptionOf(account application.CloudAccount) (string, error) {
	subscription := account.Target["subscription_id"]
	if !uuidLower.MatchString(subscription) || !uuidLower.MatchString(account.Target["tenant_id"]) {
		return "", errors.New("la sesión de Azure no trae una suscripción y un directorio válidos")
	}
	return subscription, nil
}

// deployment es un despliegue de suscripción listo para pasar a `az`: todos sus argumentos ya están validados.
type deployment struct {
	subscription string
	template     *template
	// args son los argumentos comunes a la vista previa y a la aplicación, sin la plantilla.
	args []string
	seal string
}

// prepare valida cada dato que acabará en el argv de `az` y vuelve a aplicar las reglas estáticas a la plantilla.
func prepare(req application.BootstrapRequest) (*deployment, error) {
	subscription, err := subscriptionOf(req.Account)
	if err != nil {
		return nil, err
	}
	switch {
	case !deploymentPattern.MatchString(req.Name):
		return nil, fmt.Errorf("el nombre del despliegue (%q) supera lo que Azure admite (64 caracteres): "+
			"usa un nombre de ambiente más corto", clean(req.Name, 80))
	case !uuidLower.MatchString(req.ProjectID), !slugPattern.MatchString(req.ProjectSlug),
		!application.ValidEnvironmentName(req.Environment), !regionPattern.MatchString(req.Region):
		return nil, errors.New("el proyecto, el ambiente o la región no tienen el formato esperado")
	}
	tpl, err := loadTemplate(req.Bootstrap)
	if err != nil {
		return nil, err
	}
	args := []string{
		"--name", req.Name, "--location", req.Region, "--subscription", subscription,
		"--parameters", "projectId=" + req.ProjectID, "projectSlug=" + req.ProjectSlug,
		"environment=" + req.Environment, "location=" + req.Region,
		"--no-prompt", "true", "--output", "json",
	}
	sum := sha256.Sum256([]byte(tpl.digest + "\x00" + strings.Join(args, "\x00")))
	return &deployment{subscription: subscription, template: tpl, args: args, seal: hex.EncodeToString(sum[:])}, nil
}

// run ejecuta un comando de despliegue con una copia privada de la plantilla ya comprobada: `az` lee exactamente
// los bytes que pasaron las reglas, no lo que haya en la carpeta del pipeline en ese momento.
func (b bootstrapper) run(ctx context.Context, d *deployment, command ...string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "vex-bootstrap-")
	if err != nil {
		return nil, fmt.Errorf("preparar la plantilla: %w", err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "main.json")
	if err := os.WriteFile(path, d.template.raw, 0o600); err != nil {
		return nil, fmt.Errorf("preparar la plantilla: %w", err)
	}
	args := append(command, "--template-file", path)
	return b.az.Run(ctx, append(args, d.args...)...)
}

func (b bootstrapper) Preview(ctx context.Context, req application.BootstrapRequest) (application.BootstrapPreview, error) {
	d, err := prepare(req)
	if err != nil {
		return application.BootstrapPreview{}, err
	}
	raw, err := b.run(ctx, d, "deployment", "sub", "what-if", "--result-format", "FullResourcePayloads", "--no-pretty-print")
	if err != nil {
		return application.BootstrapPreview{}, err
	}
	changes, err := parseWhatIf(raw, d.subscription)
	if err != nil {
		return application.BootstrapPreview{}, err
	}
	return application.BootstrapPreview{Declared: d.template.declared, Changes: changes, Seal: d.seal}, nil
}

func (b bootstrapper) Apply(ctx context.Context, req application.BootstrapRequest, seal string) (application.ConnectionOutputs, error) {
	none := application.ConnectionOutputs{}
	d, err := prepare(req)
	if err != nil {
		return none, err
	}
	if seal == "" || d.seal != seal {
		return none, fmt.Errorf("%w: la plantilla o sus parámetros ya no son los de la vista previa; no se aplicó nada",
			application.ErrBootstrapChanged)
	}
	raw, err := b.run(ctx, d, "deployment", "sub", "create")
	if err != nil {
		return none, err
	}
	return readOutputs(raw, req)
}

// readOutputs convierte las salidas del despliegue en los datos de la conexión y los valida con el mismo
// validador que usa cada ejecución: lo que no cumple el contrato no llega al portal.
func readOutputs(raw []byte, req application.BootstrapRequest) (application.ConnectionOutputs, error) {
	none := application.ConnectionOutputs{}
	doc, err := parseObject(raw)
	if err != nil {
		return none, fmt.Errorf("el despliegue devolvió algo que no se entiende: %w", err)
	}
	properties, _ := doc.obj("properties")
	if state, _ := properties.str("provisioningState"); state != "Succeeded" {
		return none, fmt.Errorf("el despliegue no terminó bien (estado %q)", clean(state, 40))
	}
	declared, _ := properties.obj("outputs")

	outputs := application.ConnectionOutputs{
		ContractID: ContractV1,
		Params: map[string]string{
			"tenant_id":       req.Account.Target["tenant_id"],
			"subscription_id": req.Account.Target["subscription_id"],
			"location":        req.Region,
		},
		Extras: map[string]string{},
	}
	for name, value := range declared {
		output, _ := value.(object)
		kind, _ := output.str("type")
		if strings.HasPrefix(strings.ToLower(kind), "secure") {
			return none, fmt.Errorf("la plantilla devolvió una salida segura (%q): una conexión no guarda secretos", clean(name, 60))
		}
		text, isText := output.str("value")
		if !isText || !strings.EqualFold(kind, "string") {
			continue // solo las salidas de texto forman parte de la conexión
		}
		if key, known := outputParams[name]; known {
			outputs.Params[key] = text
		} else if extraNamePattern.MatchString(name) {
			outputs.Extras[name] = text
		}
	}
	if _, _, err := (Adapter{}).Env(outputs, application.ContainerTokenPath); err != nil {
		return none, fmt.Errorf("lo que devolvió la plantilla no cumple el contrato %s: %w", ContractV1, err)
	}
	// Las identidades deben vivir en la suscripción de la sesión: vex no crea confianzas fuera de ella.
	for _, key := range []string{"deploy_identity_id", "pull_identity_id"} {
		id, present := outputs.Params[key]
		if !present {
			continue
		}
		if ref, ok := parseIdentity(id); !ok || ref.subscription != outputs.Params["subscription_id"] {
			return none, fmt.Errorf("la plantilla devolvió %q en otra suscripción que la de la sesión", key)
		}
	}
	return outputs, nil
}
