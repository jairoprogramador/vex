package cloudsession

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/jairoprogramador/vex/internal/application"
)

// staleAfter es la edad a partir de la cual un directorio de credenciales es de una ejecución que murió sin
// cerrar. Una sesión viva toca el suyo en cada renovación, mucho antes.
const staleAfter = time.Hour

// Credentials abre sesiones de credenciales de nube contra una fuente de conexiones y otra de tokens (hoy, el
// portal para las dos). Es común a todas las nubes: lo propio de cada una lo aporta el adaptador del contrato.
type Credentials struct {
	conns    application.CloudConnections
	tokens   application.TokenSource
	adapters application.CloudAdapters
	baseDir  string
	clock    Clock
	warn     func(string)
}

var _ application.CloudCredentials = (*Credentials)(nil)

// NewCredentials arma la fuente. baseDir es donde vive el directorio de cada ejecución (ver DefaultBaseDir): va
// aparte del espacio de trabajo del motor, para que ningún paso lo vea salvo por el montaje de solo lectura.
// warn recibe los avisos para el usuario; nunca llevan el token.
func NewCredentials(
	conns application.CloudConnections, tokens application.TokenSource, adapters application.CloudAdapters,
	baseDir string, warn func(string),
) *Credentials {
	return &Credentials{conns: conns, tokens: tokens, adapters: adapters, baseDir: baseDir, clock: SystemClock{}, warn: warn}
}

// WithClock cambia el reloj de la renovación; sirve para probarla sin esperas.
func (c *Credentials) WithClock(clock Clock) *Credentials {
	c.clock = clock
	return c
}

// Open prepara la sesión de un intento: comprueba la conexión, calcula las variables del contrato y crea el
// directorio que se monta. No pide ningún token: el token solo existe mientras corre un paso de nube
// (Session.StepRunning).
func (c *Credentials) Open(ctx context.Context, req application.CloudSessionRequest) (application.CloudSession, error) {
	if err := SweepStale(c.baseDir, staleAfter, c.clock.Now()); err != nil {
		c.warn(fmt.Sprintf("No se pudieron limpiar credenciales de ejecuciones anteriores: %v", err))
	}

	conn, err := c.connection(ctx, req)
	if err != nil {
		return nil, err
	}
	adapter, err := c.adapters.Lookup(conn.Outputs.ContractID)
	if err != nil {
		return nil, err
	}
	env, dropped, err := adapter.Workload().Env(*conn.Outputs, application.ContainerTokenPath)
	if err != nil {
		return nil, fmt.Errorf("la conexión de nube de '%s' no cumple el contrato %s: %w. %s",
			req.Environment, conn.Outputs.ContractID, err, "Ejecuta `vex bootstrap "+req.Environment+"`")
	}
	if len(dropped) > 0 {
		c.warn(fmt.Sprintf("La conexión de nube trae datos que esta versión de vex no conoce y no se entregan al pipeline: %s",
			strings.Join(dropped, ", ")))
	}
	env[application.CloudContractEnv] = string(conn.Outputs.ContractID)

	runID, err := NewRunID()
	if err != nil {
		return nil, err
	}
	file, err := NewTokenFile(c.baseDir, runID, TokenFileName)
	if err != nil {
		return nil, err
	}
	fetch := func(ctx context.Context) (application.IssuedToken, error) {
		return c.tokens.Issue(ctx, application.TokenRequest{
			ProjectID: req.ProjectID, Environment: req.Environment, AttemptID: req.AttemptHint, Commit: req.Commit,
		})
	}
	cloudSteps := make(map[string]bool, len(req.CloudSteps))
	for _, step := range req.CloudSteps {
		cloudSteps[step] = true
	}
	return &Session{
		env: env, file: file, cloudSteps: cloudSteps,
		refresher: NewRefresher(fetch, file, c.clock, c.warn),
	}, nil
}

// connection devuelve la conexión activa del ambiente: la que ya trae la petición o, si no, la que diga la
// fuente. Sin conexión activa no hay sesión.
func (c *Credentials) connection(ctx context.Context, req application.CloudSessionRequest) (application.CloudConnectionInfo, error) {
	var conn application.CloudConnectionInfo
	if req.Connection != nil {
		conn = *req.Connection
	} else {
		var err error
		if conn, err = c.conns.CloudConnection(ctx, req.ProjectID, req.Environment); err != nil {
			return application.CloudConnectionInfo{}, fmt.Errorf("consultar la conexión de nube: %w", err)
		}
	}
	if conn.Status != application.ConnectionActive || conn.Outputs == nil {
		return application.CloudConnectionInfo{}, &application.ConnectionNotReadyError{
			Environment: req.Environment, Status: conn.Status, Hint: conn.Hint,
		}
	}
	return conn, nil
}

// Session es la credencial de nube de un intento. El directorio existe (vacío) durante todo el intento; el
// token, solo mientras corre un paso de nube.
//
// La sesión se entera de los pasos por los eventos del motor, que pueden perderse si nadie lee su salida a
// tiempo. Por eso no cuenta eventos: cualquier noticia de un paso dice en cuál está el motor.
//   - Si se pierde el final de un paso de nube, el token se retira con la primera noticia del paso siguiente
//     y, pase lo que pase, al cerrar la sesión.
//   - Si se pierde el comienzo de un paso de nube, el token aparece con la siguiente noticia de ese paso (el
//     final de su primer comando).
//
// Lo que no se puede recuperar: si se pierden a la vez el final de un paso de nube y todas las noticias del
// siguiente, este último corre con el token todavía montado.
type Session struct {
	env        map[string]string
	file       *TokenFile
	refresher  *Refresher
	cloudSteps map[string]bool

	// mu protege active y closed: los eventos del motor y el cierre llegan de goroutines distintas.
	mu     sync.Mutex
	active string // paso de nube en curso; "" si el token no está disponible
	closed bool
}

var (
	_ application.CloudSession     = (*Session)(nil)
	_ application.StepAwareSession = (*Session)(nil)
)

// Env son las variables del contrato. Valen para todo el intento: las ven también los pasos que no son de nube.
// Lo único acotado por paso es el archivo del token.
func (s *Session) Env() map[string]string { return maps.Clone(s.env) }

// Mounts monta el directorio, no el archivo: cada renovación reemplaza el archivo y un montaje del archivo
// seguiría viendo el anterior. Es de solo lectura: el contenedor no puede reescribir el token.
func (s *Session) Mounts() []application.Mount {
	return []application.Mount{{Source: s.file.Dir(), Target: application.ContainerCredentialsDir, ReadOnly: true}}
}

// StepRunning deja el token disponible si step es un paso de nube y lo retira si no lo es. Al volver sin error
// de un paso de nube, el primer token ya está escrito y se renueva solo.
func (s *Session) StepRunning(ctx context.Context, step string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch {
	case s.closed:
		return nil
	case s.active == step:
		// Si la fuente rechazó renovar, el token vigente vencerá: quien llama decide si seguir.
		return s.refresher.Err()
	}
	if err := s.retire(); err != nil { // se perdió el final del paso anterior
		return err
	}
	if !s.cloudSteps[step] {
		return nil
	}
	if err := s.refresher.Start(ctx); err != nil {
		_ = s.file.Clear()
		return err
	}
	s.active = step
	return nil
}

// StepFinished retira el token. No mira el nombre: si un paso terminó, ningún token anterior debe seguir ahí.
func (s *Session) StepFinished(string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.retire()
}

func (s *Session) retire() error {
	if s.active == "" {
		return nil
	}
	s.refresher.Stop()
	s.active = ""
	return s.file.Clear()
}

// Close detiene la renovación y borra el directorio, con o sin paso en curso. Es idempotente.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.refresher.Stop()
	s.active, s.closed = "", true
	return s.file.Close()
}
