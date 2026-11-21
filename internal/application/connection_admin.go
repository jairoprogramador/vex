package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Lo que sigue es de vex, no de ninguna nube: cómo se llama un proyecto en la nube del usuario, a quién
// representa un token y cómo se abre, se activa y se corta una conexión en el portal.

const uuidExpr = `[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`

var (
	environmentPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)
	uuidPattern        = regexp.MustCompile(`^` + uuidExpr + `$`)
	slugNoisePattern   = regexp.MustCompile(`[^a-z0-9]+`)
)

// ValidEnvironmentName dice si name sirve como ambiente de una conexión de nube: el portal y el subject de los
// tokens usan el mismo formato.
func ValidEnvironmentName(name string) bool { return environmentPattern.MatchString(name) }

// ProjectSlug es el nombre corto del proyecto ([a-z0-9-]{1,20}) con el que la plantilla del pipeline nombra lo
// que crea. El portal lo calcula igual; los dos recorren los mismos vectores (slug-vectors.json).
func ProjectSlug(name, projectID string) string {
	unaccented := strings.Map(func(r rune) rune {
		if unicode.Is(unicode.M, r) {
			return -1
		}
		return r
	}, norm.NFD.String(name))
	base := strings.Trim(slugNoisePattern.ReplaceAllString(strings.ToLower(unaccented), "-"), "-")
	if len(base) > 13 {
		base = strings.TrimRight(base[:13], "-")
	}
	if base == "" {
		base = "proj"
	}
	id := strings.ToLower(strings.ReplaceAll(projectID, "-", ""))
	if len(id) > 6 {
		id = id[:6]
	}
	return base + "-" + id
}

// maxOperationName es lo más largo que la nube admite para el nombre de la operación.
const maxOperationName = 64

// BootstrapOperationName es el nombre con el que el bootstrap de un ambiente queda registrado en la nube:
// vex-bootstrap-<slug>-<ambiente>. Si no cabe, el ambiente se recorta y se le añade un sufijo corto de su hash:
// el nombre sigue siendo el mismo para el mismo ambiente y distinto para dos ambientes que compartan el comienzo.
func BootstrapOperationName(slug, environment string) string {
	prefix := "vex-bootstrap-" + slug + "-"
	if len(prefix)+len(environment) <= maxOperationName {
		return prefix + environment
	}
	sum := sha256.Sum256([]byte(environment))
	suffix := "-" + hex.EncodeToString(sum[:])[:6]
	room := max(maxOperationName-len(prefix)-len(suffix), 0)
	return prefix + strings.TrimRight(environment[:min(room, len(environment))], "-") + suffix
}

// ConnectionSubject es a quién representan los tokens de una conexión. Lo fija el portal al crearla; la CLI lo
// recalcula para no fiarse de lo que le digan y para auditar una conexión de la que solo conoce el id.
func ConnectionSubject(projectID, environment, connectionID string) (string, error) {
	if !uuidPattern.MatchString(projectID) || !uuidPattern.MatchString(connectionID) || !ValidEnvironmentName(environment) {
		return "", errors.New("el proyecto, el ambiente o la conexión no tienen el formato esperado")
	}
	return "vex:p:" + projectID + ":e:" + environment + ":c:" + connectionID, nil
}

// ConnectionDraft abre (o retoma) una conexión: qué bootstrap se va a aceptar y contra qué destino.
type ConnectionDraft struct {
	ProjectID       string
	Environment     string
	Contract        ContractID
	BootstrapSHA256 string
	PipelineCommit  string
	// Reconnect sustituye la conexión vigente del ambiente, que sigue activa hasta que la nueva se verifica.
	Reconnect bool
	// Target son los datos del destino que el contrato declara al portal; sus claves las pone el adaptador.
	Target map[string]string
}

// CreatedConnection es lo que el portal fija al abrir una conexión.
type CreatedConnection struct {
	ConnectionID string
	// Resumed: ya había una a medias de esta misma persona y este mismo bootstrap.
	Resumed              bool
	ReplacesConnectionID string
	Subject              string
	Issuer               string
	Audience             string
	PendingExpiresAt     time.Time
	Project              ConnectedProject
	Owner                ProjectOwner
	IsOwner              bool
}

// ConnectedProject es el proyecto tal como lo conoce el portal.
type ConnectedProject struct {
	ID   string
	Name string
	Slug string
}

// ProjectOwner es a quién pertenece el proyecto; se muestra antes de aplicar nada en la nube de alguien.
type ProjectOwner struct {
	Name  string
	Email string
}

// ConnectionCompletion entrega al portal lo que el bootstrap dejó en la nube.
type ConnectionCompletion struct {
	ConnectionID    string
	Target          map[string]string
	BootstrapSHA256 string
	Outputs         ConnectionOutputs
}

// Estados de una verificación.
const (
	VerificationActive    = "active"
	VerificationVerifying = "verifying"
	VerificationFailed    = "failed"
)

// Verification es el resultado de pedirle al portal que pruebe la conexión canjeando un token real.
type Verification struct {
	Status string
	// RetryAfter es cuánto esperar antes de volver a preguntar; cero si no hay nada que esperar.
	RetryAfter time.Duration
	// DetailCode y Message explican por qué todavía no (o ya no) funciona. Con Status "active" y DetailCode no
	// vacío, la conexión sigue activa pero esta comprobación no fue concluyente.
	DetailCode string
	Message    string
	Hint       string
}

// Conclusive dice si la verificación probó que la conexión funciona.
func (v Verification) Conclusive() bool { return v.Status == VerificationActive && v.DetailCode == "" }

// CloudConnectionAdmin es el ciclo de vida de una conexión en el portal. Solo lo usa quien administra el
// proyecto; ejecutar pasos no pasa por aquí.
type CloudConnectionAdmin interface {
	Create(ctx context.Context, draft ConnectionDraft) (CreatedConnection, error)
	// Complete es idempotente: repetirlo con los mismos datos retoma la verificación.
	Complete(ctx context.Context, completion ConnectionCompletion) (Verification, error)
	Verify(ctx context.Context, connectionID string) (Verification, error)
	// Revoke corta la emisión de tokens. Falla cerrado: sin confirmación del portal devuelve error.
	Revoke(ctx context.Context, connectionID string) error
}

// Códigos con los que el portal rechaza una operación sobre una conexión y que la CLI trata distinto.
const (
	RejectionConnectionExists     = "connection_exists"
	RejectionConnectionInProgress = "connection_in_progress"
	RejectionSignerUnavailable    = "signer_unavailable"
)

// ConnectionRejectedError: el portal entendió la petición y no la acepta. El mensaje dice qué hacer.
type ConnectionRejectedError struct {
	Code string
	// Field es el dato rechazado, cuando el portal lo dice. Nunca trae el valor.
	Field string
	// Environment lo pone quien conoce el ambiente, para que el mensaje nombre el comando exacto.
	Environment string
}

func (e *ConnectionRejectedError) Error() string {
	problem, action := e.Explain()
	if action == "" {
		return problem
	}
	return problem + ": " + action
}

// Explain separa qué pasó de qué hacer; action viene vacío cuando no hay nada que la persona pueda ejecutar.
func (e *ConnectionRejectedError) Explain() (problem, action string) {
	again := "vuelve a ejecutar `vex bootstrap " + e.environment() + "`"
	switch e.Code {
	case "contract_not_supported":
		return "el portal no admite el contrato de nube de este pipeline", ""
	case RejectionConnectionExists:
		return "el ambiente ya tiene una conexión de nube más reciente que esta", again
	case RejectionConnectionInProgress:
		return "otra persona (u otra terminal) está conectando este ambiente ahora mismo",
			"espera a que termine (como mucho 30 minutos) y " + again
	case "not_project_admin":
		return "solo quien administra el proyecto en el portal puede conectar o desconectar su nube", ""
	case "no_active_pipeline", "environment_not_found":
		return fmt.Sprintf("el portal no conoce el ambiente %q en el pipeline de este proyecto", e.environment()),
			"revisa el nombre con `vex envs`"
	case "bootstrap_mismatch":
		return "el bootstrap cambió desde que se abrió la conexión", again
	case "not_creator":
		return "esta conexión la empezó otra persona y solo ella puede completarla",
			"si ya no va a hacerlo, espera a que venza (30 minutos) y " + again
	case "not_found":
		return "el portal no encuentra esa conexión, o no administras su proyecto", ""
	case "invalid_state", "pending_expired":
		return "la conexión a medias venció o cambió de estado", again
	case "tenant_mismatch", "subscription_mismatch":
		return "la sesión de la nube apunta a otro destino que cuando se abrió la conexión", again
	case "signing_unavailable", RejectionSignerUnavailable:
		return "el portal no puede firmar tokens ahora mismo", "reintenta en unos minutos"
	}
	if e.Field != "" {
		return fmt.Sprintf("el portal rechazó un dato de la conexión (%s: %s)", e.Field, e.Code), ""
	}
	return "el portal rechazó la operación (" + e.Code + ")", ""
}

func (e *ConnectionRejectedError) environment() string {
	if e.Environment == "" {
		return "<ambiente>"
	}
	return e.Environment
}

// forEnvironment hace que un rechazo del portal nombre el ambiente en su instrucción.
func forEnvironment(err error, environment string) error {
	var rejected *ConnectionRejectedError
	if errors.As(err, &rejected) && rejected.Environment == "" {
		rejected.Environment = environment
	}
	return err
}
