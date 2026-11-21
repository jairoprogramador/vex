package application

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// LabeledValue es un dato para mostrar con su etiqueta.
type LabeledValue struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// CloudAccount es la sesión de la nube en este equipo: con quién y contra qué se va a aplicar el bootstrap. Esa
// sesión nunca sale de la máquina; al portal solo viaja Target.
type CloudAccount struct {
	// Summary es lo que se enseña antes de aplicar (cuenta, directorio, suscripción…), ya sin caracteres de control.
	Summary []LabeledValue
	// Target son los datos del destino que el contrato declara al portal. Una conexión guarda las mismas claves
	// entre sus parámetros: así se sabe si una sesión apunta al destino de una conexión.
	Target map[string]string
}

// SameTarget dice si la sesión apunta al mismo destino que describen los parámetros de una conexión.
func (a CloudAccount) SameTarget(params map[string]string) bool {
	if len(a.Target) == 0 {
		return false
	}
	for key, value := range a.Target {
		if !equalFoldNonEmpty(params[key], value) {
			return false
		}
	}
	return true
}

// Trust es la confianza de vex en la nube del usuario: de quién se aceptan tokens. Es lo único del bootstrap que
// es de vex; lo demás lo decide la plantilla del pipeline.
type Trust struct {
	Issuer   string
	Subject  string
	Audience string
}

// BootstrapRequest es qué bootstrap se aplica, con qué sesión y con qué nombres.
type BootstrapRequest struct {
	Account   CloudAccount
	Bootstrap PipelineBootstrap
	// Name es el nombre de la operación en la nube (BootstrapOperationName).
	Name        string
	ProjectID   string
	ProjectSlug string
	Environment string
	Region      string
}

// ChangeAction es qué le pasaría a un recurso si se aplica el bootstrap.
type ChangeAction string

const (
	ChangeCreate ChangeAction = "create"
	ChangeModify ChangeAction = "modify"
	ChangeNone   ChangeAction = "none"
	// ChangeIgnored: existe y la nube dice que el bootstrap no lo toca.
	ChangeIgnored ChangeAction = "ignored"
	// ChangeUnknown: la nube no supo previsualizarlo. Nunca se esconde: quien acepta debe saber qué no pudo ver.
	ChangeUnknown ChangeAction = "unknown"
)

// PlannedChange es un recurso de la vista previa. Kind, Name y Note vienen de la plantilla o de la nube: el
// adaptador los entrega ya sin caracteres de control.
type PlannedChange struct {
	Action ChangeAction
	// ID es el identificador exacto del recurso en la nube.
	ID   string
	Kind string
	Name string
	// Note explica el cambio cuando hace falta (por qué no se pudo previsualizar).
	Note string
	// Grant viene cuando el recurso es un permiso.
	Grant *PlannedGrant
}

// PlannedGrant es un permiso: qué identidad recibe qué rol y sobre qué.
type PlannedGrant struct {
	Identity string `json:"identity"`
	Role     string `json:"role"`
	Scope    string `json:"scope"`
	// Elevated: da control sobre permisos o alcanza más que un grupo de recursos, o no se pudo determinar que no.
	// Aceptarlo exige confirmación escrita.
	Elevated bool   `json:"elevated"`
	Reason   string `json:"reason,omitempty"`
}

// BootstrapPreview es lo que el bootstrap haría, en un modelo que no depende de la nube.
type BootstrapPreview struct {
	// Declared son los permisos que declara la plantilla, leídos sin llamar a la nube: se ven aunque la vista
	// previa no pueda resolverlos.
	Declared []PlannedGrant
	Changes  []PlannedChange
	// Seal identifica exactamente lo que se previsualizó; Apply solo aplica eso.
	Seal string
}

// HasChanges dice si aplicar el bootstrap crearía o modificaría algo.
func (p BootstrapPreview) HasChanges() bool {
	for _, change := range p.Changes {
		if change.Action == ChangeCreate || change.Action == ChangeModify {
			return true
		}
	}
	return false
}

// Unpreviewed son los recursos que la nube no supo previsualizar.
func (p BootstrapPreview) Unpreviewed() []PlannedChange {
	var unknown []PlannedChange
	for _, change := range p.Changes {
		if change.Action == ChangeUnknown {
			unknown = append(unknown, change)
		}
	}
	return unknown
}

// ElevatedGrants son los permisos que exigen confirmación escrita: los que declara la plantilla y los que la
// vista previa crearía o cambiaría (o no supo resolver).
func (p BootstrapPreview) ElevatedGrants() []PlannedGrant {
	var elevated []PlannedGrant
	for _, grant := range p.Declared {
		if grant.Elevated {
			elevated = append(elevated, grant)
		}
	}
	for _, change := range p.Changes {
		if change.Grant != nil && change.Grant.Elevated && change.Action != ChangeNone && change.Action != ChangeIgnored {
			elevated = append(elevated, *change.Grant)
		}
	}
	return elevated
}

// BootstrapRejectedError: el bootstrap del pipeline rompe una de las reglas que vex impone sea cual sea la
// plantilla. No se aplica nada.
type BootstrapRejectedError struct {
	// Rule es la regla (S1…S7 sobre la plantilla, V1…V6 sobre la vista previa).
	Rule   string
	Detail string
}

func (e *BootstrapRejectedError) Error() string {
	return fmt.Sprintf("El bootstrap del pipeline no se puede aplicar (regla %s): %s", e.Rule, e.Detail)
}

// BootstrapProvider aplica en una nube el bootstrap que trae el pipeline, con la sesión de quien lo acepta.
type BootstrapProvider interface {
	// Account detecta la sesión de la nube. Sin herramienta o sin sesión, el error dice qué ejecutar.
	Account(ctx context.Context) (CloudAccount, error)
	// Region decide dónde se aplica: la pedida, o la de la conexión que ya existe.
	Region(requested string, current *ConnectionOutputs) (string, error)
	// ValidateTemplate aplica las reglas estáticas, sin llamar a la nube.
	ValidateTemplate(bootstrap PipelineBootstrap) error
	// RegisterProviders deja registrados los servicios que el manifiesto pide.
	RegisterProviders(ctx context.Context, account CloudAccount, namespaces []string) error
	// Preview calcula qué cambiaría, sin cambiar nada. Si la vista previa rompe una regla devuelve
	// *BootstrapRejectedError.
	Preview(ctx context.Context, req BootstrapRequest) (BootstrapPreview, error)
	// Apply aplica exactamente lo previsualizado (seal) y devuelve lo que quedó, ya validado contra el contrato.
	Apply(ctx context.Context, req BootstrapRequest, seal string) (ConnectionOutputs, error)
}

// TrustProvider administra la confianza de vex sobre la identidad que dejó el bootstrap.
type TrustProvider interface {
	// Audience es la audiencia que el contrato exige a los tokens.
	Audience() string
	// Ensure deja la confianza want y solo esa. Si la identidad ya confía en alguien más, se detiene; solo
	// sustituye la confianza de otra conexión de este mismo proyecto y ambiente.
	Ensure(ctx context.Context, account CloudAccount, outputs ConnectionOutputs, want Trust) error
	// Exposure dice, antes de crear la confianza, qué puede hacer ya la identidad de despliegue que la vista previa
	// aceptada no enseñó: lee sus permisos efectivos y devuelve los que no se declararon (todos, si la identidad ya
	// existía y aún no confiaba en vex). Aceptarlos exige confirmación escrita. Si una identidad que devolvió el
	// bootstrap no aparece en la vista previa como creada por él, devuelve *BootstrapRejectedError.
	Exposure(ctx context.Context, account CloudAccount, preview BootstrapPreview, outputs ConnectionOutputs, want Trust) ([]PlannedGrant, error)
	// Inspect compara, sin cambiar nada, la confianza que hay con want.
	Inspect(ctx context.Context, account CloudAccount, outputs ConnectionOutputs, want Trust) ([]TrustFinding, error)
	// Remove retira la confianza de vex. Si la nube lo impide con un bloqueo devuelve *TrustLockedError.
	Remove(ctx context.Context, account CloudAccount, outputs ConnectionOutputs, want Trust) error
	// Cleanup son los comandos para terminar a mano; nunca se ejecutan.
	Cleanup(outputs ConnectionOutputs, operation string) CleanupGuide
}

// TrustFinding es una comprobación sobre la confianza.
type TrustFinding struct {
	OK   bool
	Text string
}

// CleanupGuide son comandos que la persona puede ejecutar; vex solo los imprime.
type CleanupGuide struct {
	// RemoveTrust retira la confianza de vex.
	RemoveTrust []string
	// DeleteResources borra lo que creó el bootstrap. No es de vex: es de quien lo aplicó.
	DeleteResources []string
}

// TrustLockedError: la nube impide retirar la confianza por un bloqueo que vex no quita por su cuenta.
type TrustLockedError struct {
	// Commands son los pasos exactos: quitar el bloqueo, retirar la confianza y volver a ponerlo.
	Commands []string
}

func (e *TrustLockedError) Error() string {
	return "un bloqueo de la nube impide retirar la confianza de vex"
}

// AcceptanceRecord deja constancia local de qué se aceptó aplicar en la nube. No lleva secretos.
type AcceptanceRecord struct {
	At             time.Time      `json:"at"`
	Action         string         `json:"action"` // bootstrap | disconnect
	Outcome        string         `json:"outcome"`
	ProjectID      string         `json:"project_id"`
	Environment    string         `json:"environment"`
	Contract       string         `json:"contract,omitempty"`
	ConnectionID   string         `json:"connection_id,omitempty"`
	Account        []LabeledValue `json:"account,omitempty"`
	PipelineURL    string         `json:"pipeline_url,omitempty"`
	PipelineCommit string         `json:"pipeline_commit,omitempty"`
	Fingerprint    string         `json:"bootstrap_sha256,omitempty"`
	Region         string         `json:"region,omitempty"`
	// ElevatedGrants son los permisos resaltados que se aceptaron por escrito.
	ElevatedGrants []PlannedGrant `json:"elevated_grants,omitempty"`
	// ExposedGrants son los permisos que la identidad ya tenía sin que la vista previa los mostrara, aceptados
	// por escrito antes de crear la confianza.
	ExposedGrants []PlannedGrant `json:"exposed_grants,omitempty"`
	Unpreviewed   int            `json:"unpreviewed,omitempty"`
}

// BootstrapJournal guarda las aceptaciones.
type BootstrapJournal interface {
	Record(record AcceptanceRecord) error
}

func equalFoldNonEmpty(a, b string) bool {
	return a != "" && strings.EqualFold(a, b)
}
