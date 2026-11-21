package cloudsession

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jairoprogramador/vex/internal/application"
)

const (
	retryBase = 2 * time.Second
	retryMax  = 30 * time.Second
	// expiryWarning: por debajo de este margen, un fallo al renovar ya merece un aviso.
	expiryWarning = 2 * time.Minute
)

// ErrAlreadyStarted: se llamó a Start sin haber llamado a Stop desde el Start anterior.
var ErrAlreadyStarted = errors.New("el renovador del token de nube ya está iniciado")

// FetchFunc pide un token nuevo; normalmente envuelve un application.TokenSource con su petición.
type FetchFunc func(ctx context.Context) (application.IssuedToken, error)

// TokenSink es donde se deja el token vigente.
type TokenSink interface {
	Write(token string) error
}

// Refresher mantiene en el destino un token vigente: obtiene el primero y lo renueva en segundo plano cuando la
// fuente lo indica. No sabe de pasos ni de archivos: quien lo usa lo arranca y lo detiene las veces que haga
// falta y decide cuándo borrar el token.
type Refresher struct {
	fetch FetchFunc
	sink  TokenSink
	clock Clock
	warn  func(string)

	// mu protege el ciclo de vida: cancel y done existen entre Start y Stop.
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}

	errMu sync.Mutex
	err   error
}

// NewRefresher arma un renovador. warn recibe los avisos para el usuario; nunca llevan el token.
func NewRefresher(fetch FetchFunc, sink TokenSink, clock Clock, warn func(string)) *Refresher {
	return &Refresher{fetch: fetch, sink: sink, clock: clock, warn: warn}
}

// Start obtiene y escribe el primer token antes de volver, y arranca la renovación en segundo plano, que dura
// hasta Stop o hasta que ctx se cancele. Si ya estaba iniciado devuelve ErrAlreadyStarted. Tras Stop se puede
// volver a llamar.
func (r *Refresher) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cancel != nil {
		return ErrAlreadyStarted
	}
	r.setErr(nil)

	first, err := r.fetch(ctx)
	if err != nil {
		return fmt.Errorf("obtener el token de nube: %w", err)
	}
	if err := r.sink.Write(first.Value); err != nil {
		return err
	}

	loopCtx, cancel := context.WithCancel(ctx)
	r.cancel, r.done = cancel, make(chan struct{})
	go r.run(loopCtx, first, r.done)
	return nil
}

// Stop detiene la renovación y espera a que termine. Es idempotente. No borra el token.
func (r *Refresher) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cancel == nil {
		return
	}
	r.cancel()
	<-r.done
	r.cancel, r.done = nil, nil
}

// Err devuelve el error definitivo que detuvo la renovación, si lo hubo: desde entonces el token ya no se
// renueva y vencerá.
func (r *Refresher) Err() error {
	r.errMu.Lock()
	defer r.errMu.Unlock()
	return r.err
}

func (r *Refresher) setErr(err error) {
	r.errMu.Lock()
	defer r.errMu.Unlock()
	r.err = err
}

// run renueva current una y otra vez hasta que ctx se cancele o la fuente lo rechace de forma definitiva.
func (r *Refresher) run(ctx context.Context, current application.IssuedToken, done chan<- struct{}) {
	defer close(done)

	var (
		retry   backoff
		pending *application.IssuedToken // obtenido pero aún sin escribir
		warned  bool
		wait    = r.until(current.RefreshAfter)
	)
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.clock.After(wait):
		}

		err := r.renew(ctx, &pending)
		switch {
		case ctx.Err() != nil:
			return
		case err == nil:
			current, pending, warned = *pending, nil, false
			wait = r.until(current.RefreshAfter)
			if wait > 0 {
				retry.reset()
			} else {
				// Un token que nace pidiendo renovarse no puede convertirse en un bucle sin pausa que agote
				// el límite de emisiones.
				wait = retry.next()
			}
		case errors.Is(err, application.ErrCloudTokenDenied):
			r.warn(fmt.Sprintf("La conexión de nube ya no es válida (%v); el despliegue puede fallar", err))
			r.setErr(err)
			return
		default:
			wait = r.retryDelay(err, retry.next(), current)
			if remaining := current.ExpiresAt.Sub(r.clock.Now()); remaining < expiryWarning && !warned {
				warned = true
				r.warn(renewalWarning(remaining))
			}
		}
	}
}

// renew obtiene un token (salvo que ya haya uno pendiente de escribir) y lo escribe. Si la escritura falla, el
// token queda en pending: el reintento no gasta otra emisión.
func (r *Refresher) renew(ctx context.Context, pending **application.IssuedToken) error {
	if *pending == nil {
		token, err := r.fetch(ctx)
		if err != nil {
			return err
		}
		*pending = &token
	}
	return r.sink.Write((*pending).Value)
}

// retryDelay es cuánto esperar tras un fallo transitorio: lo que pida la fuente si con eso aún se llega antes
// de que venza el token actual; si no, la espera creciente de siempre.
func (r *Refresher) retryDelay(err error, fallback time.Duration, current application.IssuedToken) time.Duration {
	var hinted application.RetryAfter
	if !errors.As(err, &hinted) {
		return fallback
	}
	asked := hinted.RetryAfter()
	if asked > fallback && r.clock.Now().Add(asked).Before(current.ExpiresAt) {
		return asked
	}
	return fallback
}

func (r *Refresher) until(moment time.Time) time.Duration {
	return max(moment.Sub(r.clock.Now()), 0)
}

func renewalWarning(remaining time.Duration) string {
	if remaining <= 0 {
		return "No se pudo renovar el token de nube; el actual ya venció"
	}
	return fmt.Sprintf("No se pudo renovar el token de nube; el actual vence en %s", remaining.Round(time.Second))
}

// backoff es la espera creciente entre reintentos: 2 s, 4 s, 8 s… hasta 30 s.
type backoff struct {
	delay time.Duration
}

func (b *backoff) next() time.Duration {
	if b.delay == 0 {
		b.delay = retryBase
	} else {
		b.delay = min(b.delay*2, retryMax)
	}
	return b.delay
}

func (b *backoff) reset() { b.delay = 0 }
