package application

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"
)

// ContractID identifica el contrato de nube que exige un pipeline ("azure/v1"). Formato <nube>/v<n>.
type ContractID string

var contractIDPattern = regexp.MustCompile(`^([a-z][a-z0-9]{1,15})/v[1-9][0-9]*$`)

// Cloud devuelve la parte de la nube ("azure"); "" si el formato no es válido.
func (c ContractID) Cloud() string {
	match := contractIDPattern.FindStringSubmatch(string(c))
	if match == nil {
		return ""
	}
	return match[1]
}

// ConnectionOutputs es lo que el bootstrap dejó en la nube del usuario. El portal lo guarda sin interpretarlo y
// la aplicación tampoco lo lee: solo el adaptador del contrato sabe qué significan las claves de Params.
type ConnectionOutputs struct {
	ContractID ContractID
	Params     map[string]string
	// Extras son las salidas propias de la plantilla del pipeline, fuera del contrato. El adaptador decide cómo
	// las ve el contenedor.
	Extras map[string]string
}

// CloudConnectionInfo es el estado de la conexión de nube de un ambiente.
type CloudConnectionInfo struct {
	Status       string // absent | pending | verifying | active | failed | revoked
	ConnectionID string
	// BootstrapSHA256 y PipelineCommit dicen qué bootstrap aceptó quien creó la conexión: la huella de la carpeta
	// bootstrap/<nube>/ del pipeline y el commit de donde salió.
	BootstrapSHA256 string
	PipelineCommit  string
	// Reason dice por qué no hay una conexión utilizable o al día (no_connection | revoked | not_admin |
	// verify_failed); "" si no hay nada que decir.
	Reason string
	// Hint es el comando que lo resuelve, tal como lo redacta el portal; "" si no hay.
	Hint string
	// Outputs solo viene con Status == "active".
	Outputs *ConnectionOutputs
}

// TokenRequest es para quién se pide el token. Solo ProjectID y Environment son obligatorios; el resto queda en
// la auditoría de la emisión.
type TokenRequest struct {
	ProjectID   string
	Environment string
	AttemptID   string
	Commit      string
	ExecutionID string
}

// IssuedToken es un token de nube de vida corta y cuándo conviene renovarlo.
type IssuedToken struct {
	Value        string
	ExpiresAt    time.Time
	RefreshAfter time.Time
}

// String no muestra Value: el token nunca llega a un log ni a un mensaje de error.
func (t IssuedToken) String() string {
	return fmt.Sprintf("IssuedToken{Value: [oculto], ExpiresAt: %s, RefreshAfter: %s}",
		t.ExpiresAt.Format(time.RFC3339), t.RefreshAfter.Format(time.RFC3339))
}

// GoString cubre %#v, que no pasa por String.
func (t IssuedToken) GoString() string { return t.String() }

// CloudConnections consulta la conexión de nube de un ambiente del proyecto.
type CloudConnections interface {
	CloudConnection(ctx context.Context, projectID, environment string) (CloudConnectionInfo, error)
}

// TokenSource emite el token de vida corta. Hoy lo implementa el portal; en la ejecución remota lo implementará
// el runner, sin cambiar la sesión.
type TokenSource interface {
	Issue(ctx context.Context, req TokenRequest) (IssuedToken, error)
}

// ConnectionActive es el único estado con el que una conexión sirve para ejecutar.
const ConnectionActive = "active"

// ConnectionNotReadyError: el ambiente no tiene una conexión de nube utilizable. El mensaje dice qué ejecutar.
type ConnectionNotReadyError struct {
	Environment string
	Status      string
	// Hint es el comando que lo resuelve según el portal; vacío si no lo dijo.
	Hint string
}

func (e *ConnectionNotReadyError) Error() string {
	problem, action := e.Explain()
	return problem + " " + action
}

// Explain separa qué pasó de qué ejecutar.
func (e *ConnectionNotReadyError) Explain() (problem, action string) {
	switch e.Status {
	case "failed":
		problem = fmt.Sprintf("La última conexión de nube de '%s' no pasó la verificación.", e.Environment)
	case "revoked":
		problem = fmt.Sprintf("La conexión de nube de '%s' fue desconectada.", e.Environment)
	default:
		problem = fmt.Sprintf("No hay conexión de nube activa para '%s'.", e.Environment)
	}
	return problem, bootstrapAdvice(e.Environment, e.Hint)
}

// bootstrapAdvice es la instrucción de siempre cuando falta la conexión o no sirve. Del portal solo se acepta
// un comando de vex; cualquier otro texto se sustituye por el comando conocido.
func bootstrapAdvice(environment, hint string) string {
	if !vexCommandPattern.MatchString(hint) {
		hint = "vex bootstrap " + environment
	}
	return "Ejecuta `" + hint + "`"
}

var vexCommandPattern = regexp.MustCompile(`^vex bootstrap [a-z0-9 -]{1,60}$`)

var (
	// ErrPortalSessionInvalid: no hay sesión de vex o el portal ya no la acepta. Se arregla iniciando sesión.
	ErrPortalSessionInvalid = errors.New("la sesión de vex no es válida")
	// ErrCloudConnectionInactive: la conexión de nube dejó de estar activa (se desconectó o se revocó).
	ErrCloudConnectionInactive = errors.New("la conexión de nube no está activa")

	// ErrCloudTokenDenied es definitivo: la sesión, el permiso o la conexión no valen, y reintentar no lo arregla.
	ErrCloudTokenDenied = errors.New("la fuente de credenciales rechazó la emisión")
	// ErrCloudTokenThrottled es transitorio: se alcanzó el límite de emisiones.
	ErrCloudTokenThrottled = errors.New("límite de emisiones de credenciales")
)

// RetryAfter lo implementa el error envuelto cuando la fuente indica cuánto esperar.
type RetryAfter interface{ RetryAfter() time.Duration }
