// Package protocol implementa el lado cliente del JSON-RPC 2.0 que habla vexd.
//
// Los tipos son una copia deliberada del lenguaje publicado del motor
// (vex-engine/docs/modelo/lenguaje-publicado.md): vex no importa vex-engine.
// Los nombres de campo van sin tags json porque el contrato usa PascalCase;
// las únicas excepciones son la notificación "progreso" y "entorno".
package protocol

import "encoding/json"

const (
	JSONRPCVersion  = "2.0"
	LanguageVersion = "1"

	MethodIntentar = "intentar"
	MethodLogs     = "logs"
	MethodCancelar = "cancelar"
	MethodProgreso = "progreso"
)

// Estados de un intento.
const (
	EstadoExitoso   = "exitoso"
	EstadoFallido   = "fallido"
	EstadoCancelado = "cancelado"
)

// Estados de un paso dentro de un intento.
const (
	PasoEjecutado  = "ejecutado"
	PasoPrecargado = "precargado"
	PasoFallido    = "fallido"
	PasoCancelado  = "cancelado"
)

// Request es una petición con id. Entorno es una extensión del motor: variables
// que se añaden al entorno de cada comando y que nunca se guardan.
type Request struct {
	JSONRPC string            `json:"jsonrpc"`
	ID      string            `json:"id"`
	Method  string            `json:"method"`
	Params  any               `json:"params,omitempty"`
	Entorno map[string]string `json:"entorno,omitempty"`
}

// Notification es un mensaje sin id ni respuesta (hoy solo "cancelar").
type Notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
}

func NewRequest(id, method string, params any, entorno map[string]string) Request {
	return Request{JSONRPC: JSONRPCVersion, ID: id, Method: method, Params: params, Entorno: entorno}
}

func NewCancel() Notification {
	return Notification{JSONRPC: JSONRPCVersion, Method: MethodCancelar}
}

// Message es cualquier línea que emite el motor: una respuesta (con id) o una
// notificación de progreso (con method y sin id).
type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// IsNotification distingue progreso de la respuesta final. Una respuesta de
// error a una petición ilegible trae "id":null, que no cuenta como ausente.
func (m *Message) IsNotification() bool {
	return m.Method != "" && len(m.ID) == 0
}

type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// PeticionDeIntento son los params de "intentar".
type PeticionDeIntento struct {
	Version           string
	Ambiente          string
	Solicitante       string
	FuenteDelProyecto string
	CommitDelProyecto string
	FuenteDelPipeline string
	CommitDelPipeline string
	HastaPaso         string
	Metadatos         Metadatos
}

type Metadatos struct {
	ProjectId           string
	ProjectName         string
	ProjectOrganization string
	ProjectTeam         string
}

// Resultado es el result de "intentar". Un intento fallido o cancelado sigue
// siendo un result: solo es error lo que impide atender la petición.
type Resultado struct {
	Intento    string
	Estado     string
	Despliegue string
	Detalle    Detalle
}

type Detalle struct {
	Tiempo string
	Pasos  []Paso
}

type Paso struct {
	Nombre string
	Estado string
}

// PeticionDeLogs son los params de "logs". Resultado admite "", "exitoso" o "fallido".
type PeticionDeLogs struct {
	Version   string
	Intento   string
	Resultado string
}

type Logs struct {
	Intento  string
	Ambiente string
	Salidas  []Salida
}

type Salida struct {
	Paso     string
	Comando  string
	Exitoso  bool
	Texto    string
	Instante string
}

// Eventos de la notificación "progreso".
const (
	EventoIntentoIniciado  = "intento_iniciado"
	EventoPasoIniciado     = "paso_iniciado"
	EventoComandoTerminado = "comando_terminado"
	EventoPasoTerminado    = "paso_terminado"
)

// Progreso son los params de "progreso". No lleva salida de comandos ni valores
// de variables; la salida se lee con "logs".
type Progreso struct {
	Evento  string `json:"evento"`
	Intento string `json:"intento,omitempty"`
	Paso    string `json:"paso,omitempty"`
	Comando string `json:"comando,omitempty"`
	Estado  string `json:"estado,omitempty"`
}
