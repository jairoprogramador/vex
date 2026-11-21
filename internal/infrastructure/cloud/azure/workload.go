// Package azure es el adaptador del contrato de nube azure/v1: lo único de vex que sabe cómo se llaman las cosas
// en Azure. Traduce los datos de una conexión a las variables que ven los pasos del pipeline y los errores
// típicos de Entra y ARM a instrucciones.
package azure

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/jairoprogramador/vex/internal/application"
)

// ContractV1 es el contrato que implementa este adaptador.
const ContractV1 application.ContractID = "azure/v1"

// Los formatos son los mismos que valida el portal al registrar la conexión. Se repiten aquí porque lo que
// llega del portal acaba en el entorno de los comandos del pipeline: nada pasa sin comprobarse.
const (
	guid     = `[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`
	rgName   = `[A-Za-z0-9._()-]{0,89}[A-Za-z0-9_()-]`
	identity = `(?i)/subscriptions/` + guid + `/resourceGroups/[A-Za-z0-9._()-]{1,90}` +
		`/providers/Microsoft\.ManagedIdentity/userAssignedIdentities/[A-Za-z0-9_-]{3,128}`
)

var (
	guidPattern     = regexp.MustCompile(`^` + guid + `$`)
	groupPattern    = regexp.MustCompile(`^` + rgName + `$`)
	identityPattern = regexp.MustCompile(`^` + identity + `$`)
	regionPattern   = regexp.MustCompile(`^[a-z0-9]{3,40}$`)
	accountPattern  = regexp.MustCompile(`^[a-z0-9]{3,24}$`)
	blobPattern     = regexp.MustCompile(`^[a-z0-9-]{3,63}$`)
)

// param es un dato del contrato: su clave en la conexión, su formato y las variables con que se entrega.
type param struct {
	key      string
	format   *regexp.Regexp
	required bool
	env      []string
}

// params es el conjunto cerrado de azure/v1. Las variables van en tres capas: la identidad con los nombres
// estándar de Azure (AZURE_*), sus alias para las herramientas que leen ARM_*, y el contexto de la conexión
// (VEX_AZURE_*), sin nombres de ninguna herramienta.
var params = []param{
	{key: "tenant_id", format: guidPattern, required: true, env: []string{"AZURE_TENANT_ID", "ARM_TENANT_ID"}},
	{key: "subscription_id", format: guidPattern, required: true, env: []string{"AZURE_SUBSCRIPTION_ID", "ARM_SUBSCRIPTION_ID"}},
	{key: "deploy_client_id", format: guidPattern, required: true, env: []string{"AZURE_CLIENT_ID", "ARM_CLIENT_ID"}},
	{key: "deploy_principal_id", format: guidPattern, required: true, env: []string{"VEX_AZURE_DEPLOY_PRINCIPAL_ID"}},
	{key: "deploy_identity_id", format: identityPattern, required: true, env: []string{"VEX_AZURE_DEPLOY_IDENTITY_ID"}},
	{key: "resource_group", format: groupPattern, required: true, env: []string{"VEX_AZURE_RESOURCE_GROUP"}},
	{key: "location", format: regionPattern, required: true, env: []string{"VEX_AZURE_LOCATION"}},
	{key: "identity_resource_group", format: groupPattern, env: []string{"VEX_AZURE_IDENTITY_RESOURCE_GROUP"}},
	{key: "pull_identity_id", format: identityPattern, env: []string{"VEX_AZURE_PULL_IDENTITY_ID"}},
	{key: "pull_client_id", format: guidPattern, env: []string{"VEX_AZURE_PULL_CLIENT_ID"}},
	{key: "state_account", format: accountPattern, env: []string{"VEX_AZURE_STATE_ACCOUNT"}},
	{key: "state_container", format: blobPattern, env: []string{"VEX_AZURE_STATE_CONTAINER"}},
}

// pairs son los datos opcionales que solo tienen sentido juntos.
var pairs = [][2]string{{"pull_identity_id", "pull_client_id"}, {"state_account", "state_container"}}

const (
	maxParamBytes = 256
	// Los extras son las salidas propias de la plantilla del pipeline.
	extraEnvPrefix = "VEX_BOOTSTRAP_"
	maxExtras      = 32
	maxExtraBytes  = 512
)

var (
	extraNamePattern = regexp.MustCompile(`^[a-z][A-Za-z0-9]{0,40}$`)
	// secretLike es la forma de una credencial: ningún dato de una conexión debe parecerse a un secreto ni a un JWT.
	secretLike = regexp.MustCompile(`(?i)eyJ|AccountKey=|SharedAccessSignature|sig=`)
)

// Adapter es el adaptador de azure/v1. El valor cero usa la CLI de Azure real para el bootstrap.
type Adapter struct {
	// az ejecuta la CLI de Azure; nil es la real.
	az Runner
}

var (
	_ application.CloudAdapter     = Adapter{}
	_ application.WorkloadContract = Adapter{}
)

func New() Adapter { return Adapter{} }

// NewWithRunner construye el adaptador con otro ejecutor de `az`; sirve para probar sin tocar Azure.
func NewWithRunner(az Runner) Adapter { return Adapter{az: az} }

func (a Adapter) runner() Runner {
	if a.az == nil {
		return execRunner{}
	}
	return a.az
}

func (a Adapter) Bootstrap() application.BootstrapProvider { return bootstrapper{az: a.runner()} }
func (a Adapter) Trust() application.TrustProvider         { return truster{az: a.runner()} }

func (Adapter) Contract() application.ContractID         { return ContractV1 }
func (a Adapter) Workload() application.WorkloadContract { return a }

// Env arma las variables del contrato. Ningún valor llega al contenedor sin pasar por el formato de su clave:
// un valor extraño acabaría como argumento de `az login` o de terraform.
func (Adapter) Env(outputs application.ConnectionOutputs, tokenPath string) (map[string]string, []string, error) {
	env := map[string]string{
		"AZURE_FEDERATED_TOKEN_FILE": tokenPath,
		"ARM_OIDC_TOKEN_FILE_PATH":   tokenPath,
		"ARM_USE_OIDC":               "true",
		// La sesión de `az` vive en el directorio efímero del paso: el motor lo borra al terminar y el paso
		// siguiente no la hereda.
		"AZURE_CONFIG_DIR": application.ContainerStepTmpDir + "/azure",
	}
	known := make(map[string]bool, len(params))
	for _, p := range params {
		known[p.key] = true
		value, present := outputs.Params[p.key]
		switch {
		case !present && p.required:
			return nil, nil, fmt.Errorf("a la conexión le falta %q", p.key)
		case !present:
			continue
		case len(value) > maxParamBytes || secretLike.MatchString(value) || !p.format.MatchString(value):
			// El valor no se cita: si no tiene el formato esperado, tampoco se sabe qué es.
			return nil, nil, fmt.Errorf("el dato %q de la conexión no tiene el formato esperado", p.key)
		}
		for _, name := range p.env {
			env[name] = value
		}
	}
	for _, pair := range pairs {
		if _, first := outputs.Params[pair[0]]; first != hasKey(outputs.Params, pair[1]) {
			return nil, nil, fmt.Errorf("la conexión trae %q o %q sin su pareja", pair[0], pair[1])
		}
	}

	extras, err := extrasEnv(outputs.Extras)
	if err != nil {
		return nil, nil, err
	}
	maps.Copy(env, extras)

	var dropped []string
	for key := range outputs.Params {
		if !known[key] {
			dropped = append(dropped, key)
		}
	}
	slices.Sort(dropped)
	return env, dropped, nil
}

func hasKey(m map[string]string, key string) bool {
	_, ok := m[key]
	return ok
}

// extrasEnv expone cada salida extra de la plantilla como VEX_BOOTSTRAP_<NOMBRE>.
func extrasEnv(extras map[string]string) (map[string]string, error) {
	if len(extras) > maxExtras {
		return nil, fmt.Errorf("la conexión trae más de %d salidas extra", maxExtras)
	}
	env := make(map[string]string, len(extras))
	for name, value := range extras {
		if !extraNamePattern.MatchString(name) {
			return nil, errors.New("la conexión trae una salida extra con un nombre no válido")
		}
		if !validExtraValue(value) {
			return nil, fmt.Errorf("la salida extra %q de la conexión no es un valor de una línea de hasta %d bytes", name, maxExtraBytes)
		}
		env[extraEnvPrefix+upperSnake(name)] = value
	}
	return env, nil
}

// validExtraValue: una línea de ASCII imprimible (lo mismo que acepta el portal), sin forma de secreto.
func validExtraValue(value string) bool {
	if len(value) > maxExtraBytes || secretLike.MatchString(value) {
		return false
	}
	return !strings.ContainsFunc(value, func(r rune) bool { return r < 0x20 || r > 0x7e })
}

// upperSnake pasa un nombre en camelCase a MAYÚSCULAS_CON_GUION_BAJO, con un guion bajo antes de cada mayúscula:
// registryLoginServer → REGISTRY_LOGIN_SERVER. La regla es deliberadamente literal (appURL → APP_U_R_L): así dos
// nombres distintos nunca dan la misma variable y cualquiera puede predecirla al escribir un pipeline.
func upperSnake(name string) string {
	var out strings.Builder
	for i, r := range name {
		if i > 0 && unicode.IsUpper(r) {
			out.WriteByte('_')
		}
		out.WriteRune(unicode.ToUpper(r))
	}
	return out.String()
}

// ReservedEnvPrefixes: lo que el usuario no puede definir en runtime.run.envs cuando las credenciales salen del
// portal, porque lo pisaría la sesión (o, peor, la sesión no lo pisaría y mezclaría dos identidades).
func (Adapter) ReservedEnvPrefixes() []string {
	return []string{"ARM_", "AZURE_", "VEX_AZURE_"}
}

// ExecutionContextKey identifica el destino del despliegue: directorio, suscripción, grupo de recursos y región.
// No incluye la identidad ni el estado: cambiar de identidad sin cambiar de destino no invalida lo ya hecho.
func (Adapter) ExecutionContextKey(p map[string]string) string {
	return strings.Join([]string{p["tenant_id"], p["subscription_id"], p["resource_group"], p["location"]}, "\x00")
}

var errorHints = []application.ErrorHint{
	{
		Code:    "AADSTS70021",
		Pattern: regexp.MustCompile(`(?i)AADSTS70021\b`),
		Message: "La confianza de vex en Azure para este ambiente fue modificada o eliminada. Revísala con `vex bootstrap {env} --check`",
	},
	{
		Code:    "AADSTS700024",
		Pattern: regexp.MustCompile(`(?i)AADSTS700024`),
		Message: "El token de despliegue venció (¿la máquina se suspendió?). Vuelve a ejecutar.",
	},
	{
		Code:    "arm_403",
		Pattern: regexp.MustCompile(`(?i)AuthorizationFailed|does not have authorization to perform action`),
		Message: "Azure rechazó la operación por permisos. Si acabas de ejecutar `vex bootstrap`, los permisos pueden tardar en " +
			"propagarse: reintenta en 1 minuto. Si persiste, revisa la conexión con `vex bootstrap {env} --check`",
	},
	{
		Code:    "identidad_no_validada",
		Pattern: regexp.MustCompile(`(?i)AADSTS700211|AADSTS50166|AADSTS7000215`),
		Message: "Azure no pudo validar la identidad de vex para este ambiente. Revísala con `vex bootstrap {env} --check`",
	},
	{
		Code:    "token_no_llego",
		Pattern: regexp.MustCompile(`(?i)vex-azure-login: .*(no apareci|timeout)`),
		Message: "No se obtuvo el token de Azure para este paso. Revisa tu conexión a Internet y vuelve a ejecutar; " +
			"si persiste, vex why muestra el detalle.",
	},
	{
		Code:    "token_sin_permiso",
		Pattern: regexp.MustCompile(`(?i)vex-azure-login: .*(permiso|permission)`),
		Message: "El contenedor no puede leer el token (permisos del directorio de credenciales). " +
			"Ver docs/solucion-de-problemas.md › «Linux: permisos del token».",
	},
}

func (Adapter) ErrorHints() []application.ErrorHint { return slices.Clone(errorHints) }
