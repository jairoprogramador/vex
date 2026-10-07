package protocol

import "time"

// Operaciones de consulta y de lanzamiento del lenguaje publicado. Igual que en types.go, los nombres de campo
// son los del motor (PascalCase, sin tags) y se protegen con el test de contrato contra un vexd real.

const (
	MethodRollback     = "rollback"
	MethodSimular      = "simular"
	MethodLanzar       = "lanzar"
	MethodReservar     = "reservar"
	MethodLiberar      = "liberar"
	MethodDiagnosticar = "diagnosticar"
	MethodAbandonar    = "abandonar"
	MethodIntento      = "intento"
	MethodIntentos     = "intentos"
	MethodDespliegues  = "despliegues"
	MethodLanzamientos = "lanzamientos"
	MethodAmbientes    = "ambientes"
	MethodPasos        = "pasos"
)

// --- Peticiones ---

// PeticionPorAmbiente la comparten intentos, despliegues, lanzamientos, reservar y liberar.
type PeticionPorAmbiente struct {
	Version  string
	Ambiente string
}

// PeticionPorIntento la comparten intento y abandonar.
type PeticionPorIntento struct {
	Version string
	Intento string
}

type PeticionDeRollback struct {
	Version     string
	Despliegue  string
	Solicitante string
	Metadatos   Metadatos
}

// PeticionDeSimulacion valida sin ejecutar: la fuente es el repositorio del pipeline y Commit, su SHA completo.
type PeticionDeSimulacion struct {
	Version     string
	Ambiente    string
	Solicitante string
	HastaPaso   string
	Fuente      string
	Commit      string
	Metadatos   Metadatos
}

type PeticionDeLanzar struct {
	Version    string
	Ambiente   string
	Despliegue string
	Nombre     string
}

// PeticionDeDiagnostico: Intento y Lanzamiento son excluyentes; sin ninguno vale el último intento de Ambiente.
type PeticionDeDiagnostico struct {
	Version     string
	Intento     string
	Lanzamiento string
	Ambiente    string
	Referencia  string
}

// PeticionDeCatalogo la comparten ambientes y pasos. Commit vacío es el pipeline de hoy.
type PeticionDeCatalogo struct {
	Version           string
	FuenteDelPipeline string
	Commit            string
}

// --- Resultados ---

// ResumenDeIntento es cada elemento de la lista de intentos de un ambiente, del más antiguo al más reciente.
type ResumenDeIntento struct {
	Id          string
	Ambiente    string
	Solicitante string
	HastaPaso   string
	Instante    time.Time
	Estado      string // vacío si el intento no tiene desenlace
	Causa       string
}

type Despliegue struct {
	Id       string
	Ambiente string
	Intento  string
	Padre    string
	Instante time.Time
}

type Lanzamiento struct {
	Id         string
	Ambiente   string
	Despliegue string
	Version    int
	Nombre     string
	Instante   time.Time
}

type Ambiente struct {
	Nombre      string
	Descripcion string
	// Valor es con el que se nombra el ambiente al pedir un intento o un lanzamiento.
	Valor     string
	Reservado bool
}

type PasoDelPipeline struct {
	Nombre     string
	Orden      int
	Compartido bool
}

// ResultadoDeSimulacion: Estado "fallido" trae Causa con Fallos o con Faltante, nunca los dos.
type ResultadoDeSimulacion struct {
	Ambiente    string
	Solicitante string
	HastaPaso   string
	Estado      string
	Causa       *CausaDeSimulacion
}

type CausaDeSimulacion struct {
	Fallos   []Fallo
	Faltante []Faltante
}

// Faltante son las variables que un paso usa y no se resuelven.
type Faltante struct {
	Paso      string
	Variables []string
}

// IntentoDeHistorial es la respuesta de la operación intento: todo lo que el Historial guarda del intento.
type IntentoDeHistorial struct {
	Id         string
	Apertura   Apertura
	Instante   time.Time
	Registros  []RegistroDePaso
	Estado     string
	Causa      string
	Destino    string
	Abandonado bool
}

type Apertura struct {
	Ambiente      string
	Solicitante   string
	Pasos         []PasoDeclarado
	HastaPaso     string
	ConCommits    bool
	HashDelCodigo string
	Contenido     Contenido
}

type PasoDeclarado struct {
	Nombre     string
	Compartido bool
}

// Registros de un paso: comienzo, final o no_reejecucion.
const (
	RegistroComienzo      = "comienzo"
	RegistroFinal         = "final"
	RegistroNoReejecucion = "no_reejecucion"
)

type RegistroDePaso struct {
	Intento   string
	Paso      string
	Tipo      string
	Exitoso   bool
	Evidencia Evidencia
	Instante  time.Time
	Contenido Contenido
}

type Evidencia struct {
	Intento string
	Paso    string
}

// Contenido lo interpreta quien lo escribió; el CLI no lo lee.
type Contenido struct {
	Contexto string
	Datos    []byte
}

// SinDiagnostico dice por qué no hay diagnóstico; si viene vacío, lo hay.
const (
	SinReferencia = "sin_referencia"
	NoSeAtribuye  = "no_se_atribuye"
)

// RespuestaDeDiagnostico tiene dos formas: solo SinDiagnostico, o el diagnóstico completo.
type RespuestaDeDiagnostico struct {
	SinDiagnostico string
	Ambiente       string
	IntentoExitoso *IntentoDeReferencia
	IntentoFallido *IntentoQueFalla
	Sustento       *Sustento
}

type IntentoDeReferencia struct {
	Id    string
	Fecha time.Time
}

type IntentoQueFalla struct {
	Id    string
	Fecha time.Time
	// CantidadDeIntentos solo viene si la referencia es el último despliegue del mismo ambiente.
	CantidadDeIntentos int
}

// Sustento dice qué cambió; cada eje viene solo si cambió. Un Sustento vacío es «nada de lo que mira cada paso».
type Sustento struct {
	Codigo        *PasosCambiados
	Instrucciones *PasosCambiados
	Variables     *VariablesCambiadas
}

type PasosCambiados struct {
	Pasos []string
}

type VariablesCambiadas struct {
	DeclaradasCambiadas []VariableDeUnPaso
	ProducidasCambiadas []VariableDeUnPaso
}

type VariableDeUnPaso struct {
	Paso   string
	Nombre string
}
