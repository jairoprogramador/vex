package protocol

import (
	"encoding/json"
	"fmt"
)

// ErrorKind es el "data.tipo" estable del catálogo de errores del motor. Implementa
// error para poder usarlo con errors.Is(err, protocol.ErrAmbienteOcupado).
type ErrorKind string

func (k ErrorKind) Error() string { return string(k) }

const (
	ErrJSONInvalido          ErrorKind = "json_invalido"
	ErrPeticionInvalida      ErrorKind = "peticion_invalida"
	ErrOperacionDesconocida  ErrorKind = "operacion_desconocida"
	ErrParametrosInvalidos   ErrorKind = "parametros_invalidos"
	ErrInterno               ErrorKind = "interno"
	ErrVersionNoSoportada    ErrorKind = "version_no_soportada"
	ErrRechazado             ErrorKind = "rechazado"
	ErrNoExiste              ErrorKind = "no_existe"
	ErrAmbienteOcupado       ErrorKind = "ambiente_ocupado"
	ErrNoDisponible          ErrorKind = "no_disponible"
	ErrConfiguracionInvalida ErrorKind = "configuracion_invalida"
	ErrCancelado             ErrorKind = "cancelado"
	ErrEscrituraConcurrente  ErrorKind = "escritura_concurrente"
	ErrHistorialSinIntentos  ErrorKind = "historial_sin_intentos"
)

// Fallo es un incumplimiento de invariante del pipeline (error "rechazado").
type Fallo struct {
	Invariante string
	Fichero    string
	Paso       string
	Ambiente   string
	Detalle    string
}

// ErrorData son los datos que el motor adjunta a un error.
type ErrorData struct {
	Tipo     ErrorKind `json:"tipo"`
	Ambiente string    `json:"ambiente,omitempty"`
	Intento  string    `json:"intento,omitempty"`
	Variable string    `json:"variable,omitempty"`
	Fallos   []Fallo   `json:"fallos,omitempty"`
}

// EngineError es un error JSON-RPC devuelto por el motor.
type EngineError struct {
	Code    int
	Message string
	Data    ErrorData
}

func newEngineError(e *RPCError) *EngineError {
	err := &EngineError{Code: e.Code, Message: e.Message}
	if len(e.Data) > 0 {
		// Un data que no se entiende no debe tapar el error original.
		_ = json.Unmarshal(e.Data, &err.Data)
	}
	return err
}

func (e *EngineError) Error() string {
	if e.Data.Tipo == "" {
		return fmt.Sprintf("motor: %s (código %d)", e.Message, e.Code)
	}
	return fmt.Sprintf("motor: %s [%s]", e.Message, e.Data.Tipo)
}

func (e *EngineError) Kind() ErrorKind { return e.Data.Tipo }

// Is permite errors.Is(err, protocol.ErrXxx) comparando por tipo.
func (e *EngineError) Is(target error) bool {
	kind, ok := target.(ErrorKind)
	return ok && kind == e.Data.Tipo
}
