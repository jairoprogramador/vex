package application

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// El token de nube se entrega al contenedor como un archivo con el mismo nombre para cualquier nube; cada
// adaptador decide con qué variables lo anuncia.
const (
	ContainerCredentialsDir = "/vex/credenciales"
	TokenFileName           = "oidc.jwt"
	ContainerTokenPath      = ContainerCredentialsDir + "/" + TokenFileName
	// ContainerEphemeralBase es la base del directorio efímero que el motor crea vacío antes de cada paso y borra
	// al terminar; ContainerStepTmpDir es ese directorio. Lo que una herramienta guarde ahí (su sesión de nube) no
	// pasa al paso siguiente. Es del contenedor, nunca un volumen.
	ContainerEphemeralBase = "/tmp"
	ContainerStepTmpDir    = ContainerEphemeralBase + "/vex-step-tmp"
	// CloudContractEnv le dice al pipeline con qué contrato se abrió la sesión, sea cual sea la nube.
	CloudContractEnv = "VEX_CLOUD_CONTRACT"
)

// CloudAdapter agrupa lo específico de una nube para un contrato.
type CloudAdapter interface {
	Contract() ContractID
	Workload() WorkloadContract
	// Bootstrap aplica el bootstrap del pipeline con la sesión de quien lo acepta; Trust administra la confianza
	// de vex sobre lo que el bootstrap dejó. Solo los usan `vex bootstrap` y `vex disconnect`.
	Bootstrap() BootstrapProvider
	Trust() TrustProvider
}

// WorkloadContract traduce una conexión a lo que ve el contenedor de un paso.
type WorkloadContract interface {
	// Env devuelve las variables del contrato para el contenedor: solo identificadores y rutas, nunca un
	// secreto. Solo salen valores validados de claves conocidas: un dato del contrato que falta o no tiene el
	// formato esperado es un error; dropped son las claves que esta versión no conoce y no se entregan.
	Env(outputs ConnectionOutputs, tokenPath string) (env map[string]string, dropped []string, err error)
	// ReservedEnvPrefixes son los prefijos que el usuario no puede definir en runtime.run.envs.
	ReservedEnvPrefixes() []string
	// ExecutionContextKey identifica el destino (no la identidad) al que apunta una conexión.
	ExecutionContextKey(params map[string]string) string
	// ErrorHints traduce errores conocidos de la nube a instrucciones.
	ErrorHints() []ErrorHint
}

// ErrorHint es una instrucción para un error conocido de la nube.
type ErrorHint struct {
	// Code identifica la pista en el catálogo de errores (docs/solucion-de-problemas.md).
	Code string
	// Pattern se busca en la salida del comando fallido.
	Pattern *regexp.Regexp
	// Message puede contener {env}, que se sustituye por el ambiente. Nunca lleva datos de la conexión.
	Message string
}

// CloudAdapters es el registro de adaptadores por contrato. Se arma una vez, al construir los servicios: añadir
// una nube es registrar su adaptador.
type CloudAdapters map[ContractID]CloudAdapter

// NewCloudAdapters registra cada adaptador bajo su contrato.
func NewCloudAdapters(adapters ...CloudAdapter) CloudAdapters {
	registry := make(CloudAdapters, len(adapters))
	for _, adapter := range adapters {
		registry[adapter.Contract()] = adapter
	}
	return registry
}

// UnsupportedContractError: el pipeline (o la conexión) exige un contrato que esta versión de vex no conoce.
type UnsupportedContractError struct {
	Required  ContractID
	Supported []ContractID
}

func (e *UnsupportedContractError) Error() string {
	supported := make([]string, len(e.Supported))
	for i, id := range e.Supported {
		supported[i] = string(id)
	}
	return fmt.Sprintf("este pipeline requiere el contrato de nube %q y esta versión de vex soporta: %s. Actualiza vex",
		e.Required, strings.Join(supported, ", "))
}

// Lookup devuelve el adaptador del contrato, o *UnsupportedContractError con los que sí existen.
func (r CloudAdapters) Lookup(id ContractID) (CloudAdapter, error) {
	if adapter, ok := r[id]; ok {
		return adapter, nil
	}
	supported := make([]ContractID, 0, len(r))
	for known := range r {
		supported = append(supported, known)
	}
	slices.Sort(supported)
	return nil, &UnsupportedContractError{Required: id, Supported: supported}
}

// Supports dice si esta versión de vex sabe usar el contrato. La comparación es exacta.
func (r CloudAdapters) Supports(contract string) bool {
	_, ok := r[ContractID(contract)]
	return ok
}

// ErrorCatalogs son los catálogos de errores de los adaptadores que traen uno, en orden de contrato.
func (r CloudAdapters) ErrorCatalogs() []ErrorCatalog {
	ids := make([]ContractID, 0, len(r))
	for id := range r {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	var catalogs []ErrorCatalog
	for _, id := range ids {
		if catalog, ok := r[id].(ErrorCatalog); ok {
			catalogs = append(catalogs, catalog)
		}
	}
	return catalogs
}

// ErrorHints junta las instrucciones de todos los adaptadores, en orden de contrato: sirve a quien muestra la
// salida de un intento sin saber con qué nube corrió.
func (r CloudAdapters) ErrorHints() []ErrorHint {
	ids := make([]ContractID, 0, len(r))
	for id := range r {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	var hints []ErrorHint
	for _, id := range ids {
		hints = append(hints, r[id].Workload().ErrorHints()...)
	}
	return hints
}
