package cloudsession

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/jairoprogramador/vex/internal/application"
)

var start = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

// fakeClock es un reloj que solo avanza cuando el test lo dice. Cada After queda anotado en waits: leerlo es la
// forma de saber que el renovador terminó lo que estaba haciendo y volvió a dormir.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []fakeTimer
	waits  chan time.Duration
}

type fakeTimer struct {
	at time.Time
	ch chan time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: start, waits: make(chan time.Duration, 100)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- c.now
	} else {
		c.timers = append(c.timers, fakeTimer{at: c.now.Add(d), ch: ch})
	}
	c.waits <- d
	return ch
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	pending := c.timers[:0]
	for _, timer := range c.timers {
		if timer.at.After(c.now) {
			pending = append(pending, timer)
			continue
		}
		timer.ch <- c.now
	}
	c.timers = pending
}

// nextWait devuelve cuánto pidió dormir el renovador.
func (c *fakeClock) nextWait(t *testing.T) time.Duration {
	t.Helper()
	select {
	case d := <-c.waits:
		return d
	case <-time.After(5 * time.Second):
		t.Fatal("el renovador no volvió a dormir")
		return 0
	}
}

// expectWait comprueba la siguiente espera y la deja pasar.
func (c *fakeClock) expectWait(t *testing.T, want time.Duration) {
	t.Helper()
	if got := c.nextWait(t); got != want {
		t.Fatalf("espera: got %v, want %v", got, want)
	}
	c.Advance(want)
}

// fakeSource entrega, en orden, los resultados que se le programan; cuando se acaban repite el último.
type fakeSource struct {
	mu      sync.Mutex
	results []fetchResult
	calls   int
}

type fetchResult struct {
	token application.IssuedToken
	err   error
}

func (s *fakeSource) fetch(ctx context.Context) (application.IssuedToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return application.IssuedToken{}, err
	}
	result := s.results[min(s.calls, len(s.results)-1)]
	s.calls++
	return result.token, result.err
}

func (s *fakeSource) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// memorySink guarda el último token escrito; puede fallar las primeras escrituras que se le pidan.
type memorySink struct {
	mu       sync.Mutex
	current  string
	writes   int
	failures []error
}

func (s *memorySink) Write(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	if len(s.failures) > 0 {
		err := s.failures[0]
		s.failures = s.failures[1:]
		if err != nil {
			return err
		}
	}
	s.current = token
	return nil
}

func (s *memorySink) token() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

type warnings struct {
	mu   sync.Mutex
	list []string
}

func (w *warnings) add(message string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.list = append(w.list, message)
}

func (w *warnings) all() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.list...)
}

// issued es un token emitido en el instante at con la vida que da el portal: 10 minutos, renovable a los 5.
func issued(value string, at time.Time) fetchResult {
	return fetchResult{token: application.IssuedToken{
		Value: value, ExpiresAt: at.Add(10 * time.Minute), RefreshAfter: at.Add(5 * time.Minute),
	}}
}

func failed(err error) fetchResult { return fetchResult{err: err} }

var errTransient = errors.New("el portal no puede firmar ahora")

// throttled imita el error del límite de emisiones con la espera que pide la fuente.
type throttled struct{ after time.Duration }

func (e throttled) Error() string             { return "límite de emisiones" }
func (e throttled) Unwrap() error             { return application.ErrCloudTokenThrottled }
func (e throttled) RetryAfter() time.Duration { return e.after }

type rig struct {
	clock     *fakeClock
	source    *fakeSource
	sink      *memorySink
	warnings  *warnings
	refresher *Refresher
}

func newRig(t *testing.T, results ...fetchResult) *rig {
	t.Helper()
	r := &rig{clock: newFakeClock(), source: &fakeSource{results: results}, sink: &memorySink{}, warnings: &warnings{}}
	r.refresher = NewRefresher(r.source.fetch, r.sink, r.clock, r.warnings.add)
	t.Cleanup(func() {
		r.refresher.Stop()
		goleak.VerifyNone(t)
	})
	return r
}

func (r *rig) mustStart(t *testing.T) {
	t.Helper()
	if err := r.refresher.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
}

func TestRefresher_StartEscribeElPrimerTokenAntesDeVolver(t *testing.T) {
	r := newRig(t, issued(tokenA, start))

	r.mustStart(t)

	if r.sink.token() != tokenA || r.source.count() != 1 {
		t.Fatalf("token %q tras %d emisiones", r.sink.token(), r.source.count())
	}
}

func TestRefresher_RenuevaExactamenteEnRefreshAfter(t *testing.T) {
	r := newRig(t, issued(tokenA, start), issued(tokenB, start.Add(5*time.Minute)))
	r.mustStart(t)

	if got := r.clock.nextWait(t); got != 5*time.Minute {
		t.Fatalf("primera espera: got %v, want 5m", got)
	}
	r.clock.Advance(5*time.Minute - time.Second)
	if r.source.count() != 1 || r.sink.token() != tokenA {
		t.Fatal("renovó antes de tiempo")
	}

	r.clock.Advance(time.Second)

	if got := r.clock.nextWait(t); got != 5*time.Minute {
		t.Fatalf("siguiente espera: got %v, want 5m (lo que manda el token nuevo)", got)
	}
	if r.source.count() != 2 || r.sink.token() != tokenB {
		t.Fatalf("token %q tras %d emisiones", r.sink.token(), r.source.count())
	}
}

func TestRefresher_ConRefreshAfterYaPasadoRenuevaDeInmediato(t *testing.T) {
	stale := fetchResult{token: application.IssuedToken{
		Value: tokenA, ExpiresAt: start.Add(time.Minute), RefreshAfter: start.Add(-time.Minute),
	}}
	r := newRig(t, stale, issued(tokenB, start))
	r.mustStart(t)

	r.clock.expectWait(t, 0)

	if got := r.clock.nextWait(t); got != 5*time.Minute {
		t.Fatalf("espera tras renovar: got %v", got)
	}
	if r.sink.token() != tokenB {
		t.Fatalf("token: got %q", r.sink.token())
	}
}

func TestRefresher_UnaFuenteQueSiemprePideRenovarNoGiraSinPausa(t *testing.T) {
	stale := fetchResult{token: application.IssuedToken{
		Value: tokenA, ExpiresAt: start.Add(time.Hour), RefreshAfter: start.Add(-time.Minute),
	}}
	r := newRig(t, stale)
	r.mustStart(t)

	for _, want := range []time.Duration{0, 2 * time.Second, 4 * time.Second, 8 * time.Second} {
		r.clock.expectWait(t, want)
	}
}

func TestRefresher_AnteUnFalloTransitorioConservaElTokenYReintentaConEsperaCreciente(t *testing.T) {
	r := newRig(t,
		issued(tokenA, start),
		failed(errTransient), failed(errTransient), failed(errTransient), failed(errTransient),
		failed(errTransient), failed(errTransient),
		issued(tokenB, start.Add(6*time.Minute)),
		failed(errTransient),
	)
	r.mustStart(t)
	r.clock.expectWait(t, 5*time.Minute)

	for _, want := range []time.Duration{2, 4, 8, 16, 30, 30} {
		if got := r.clock.nextWait(t); got != want*time.Second {
			t.Fatalf("espera: got %v, want %vs", got, want)
		}
		if r.sink.token() != tokenA {
			t.Fatalf("un fallo no debe tocar el token vigente: %q", r.sink.token())
		}
		r.clock.Advance(want * time.Second)
	}

	// 5m + 90s de reintentos: el token nuevo se emitió en 6m y pide renovarse en 11m.
	r.clock.expectWait(t, 11*time.Minute-(5*time.Minute+90*time.Second))
	if r.sink.token() != tokenB {
		t.Fatalf("token: got %q", r.sink.token())
	}
	if got := r.clock.nextWait(t); got != 2*time.Second {
		t.Fatalf("tras un éxito la espera vuelve a empezar: got %v", got)
	}
	if err := r.refresher.Err(); err != nil {
		t.Fatalf("un fallo transitorio no es definitivo: %v", err)
	}
}

func TestRefresher_AvisaUnaSolaVezCuandoElTokenEstaPorVencer(t *testing.T) {
	short := fetchResult{token: application.IssuedToken{
		Value: tokenA, ExpiresAt: start.Add(3 * time.Minute), RefreshAfter: start.Add(time.Minute),
	}}
	r := newRig(t, short, failed(errTransient))
	r.mustStart(t)

	r.clock.expectWait(t, time.Minute) // falla con 2m de margen: todavía no avisa
	if got := r.clock.nextWait(t); got != 2*time.Second {
		t.Fatalf("espera: got %v", got)
	}
	if got := r.warnings.all(); len(got) != 0 {
		t.Fatalf("aviso prematuro: %v", got)
	}
	r.clock.Advance(2 * time.Second)
	for _, want := range []time.Duration{4, 8, 16} { // falla con 1m58s: avisa; los siguientes no repiten
		r.clock.expectWait(t, want*time.Second)
	}
	r.clock.nextWait(t)

	got := r.warnings.all()
	if len(got) != 1 {
		t.Fatalf("avisos: got %v, want uno solo", got)
	}
	if got[0] != "No se pudo renovar el token de nube; el actual vence en 1m58s" {
		t.Fatalf("aviso: %q", got[0])
	}
	assertNoToken(t, got[0])
}

func TestRefresher_UnRechazoDefinitivoDetieneLaRenovacion(t *testing.T) {
	cases := map[string]error{
		"sesión vencida (401)": fmt.Errorf("%w: portal: unauthorized", application.ErrCloudTokenDenied),
		"conexión revocada":    fmt.Errorf("%w: portal: la conexión de nube no está activa", application.ErrCloudTokenDenied),
	}
	for name, denied := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, issued(tokenA, start), failed(denied))
			r.mustStart(t)
			r.clock.expectWait(t, 5*time.Minute)

			waitFor(t, func() bool { return r.refresher.Err() != nil })

			if err := r.refresher.Err(); !errors.Is(err, application.ErrCloudTokenDenied) || !errors.Is(err, denied) {
				t.Fatalf("Err: got %v", err)
			}
			r.clock.Advance(time.Hour)
			select {
			case d := <-r.clock.waits:
				t.Fatalf("el renovador siguió vivo: pidió dormir %v", d)
			default:
			}
			if r.source.count() != 2 {
				t.Fatalf("no debe reintentar: %d emisiones pedidas", r.source.count())
			}
			if r.sink.token() != tokenA {
				t.Fatal("el token vigente no se toca")
			}
			got := r.warnings.all()
			if len(got) != 1 || !strings.HasPrefix(got[0], "La conexión de nube ya no es válida (") ||
				!strings.HasSuffix(got[0], "); el despliegue puede fallar") {
				t.Fatalf("aviso: %v", got)
			}
			assertNoToken(t, got[0])
		})
	}
}

func TestRefresher_ElLimiteDeEmisionesEsTransitorioYRespetaLaEsperaPedida(t *testing.T) {
	t.Run("si cabe antes del vencimiento, espera lo que pide la fuente", func(t *testing.T) {
		r := newRig(t, issued(tokenA, start), failed(throttled{after: 3 * time.Minute}), issued(tokenB, start.Add(8*time.Minute)))
		r.mustStart(t)
		r.clock.expectWait(t, 5*time.Minute)

		r.clock.expectWait(t, 3*time.Minute)

		r.clock.nextWait(t)
		if r.sink.token() != tokenB || r.refresher.Err() != nil {
			t.Fatalf("token %q, err %v", r.sink.token(), r.refresher.Err())
		}
	})
	t.Run("si el token vencería antes, reintenta con la espera de siempre", func(t *testing.T) {
		r := newRig(t, issued(tokenA, start), failed(throttled{after: time.Hour}))
		r.mustStart(t)
		r.clock.expectWait(t, 5*time.Minute)

		if got := r.clock.nextWait(t); got != 2*time.Second {
			t.Fatalf("espera: got %v, want 2s", got)
		}
	})
}

func TestRefresher_SiFallaLaEscrituraReintentaSinGastarOtraEmision(t *testing.T) {
	r := newRig(t, issued(tokenA, start), issued(tokenB, start.Add(5*time.Minute)))
	r.sink.failures = []error{nil, errors.New("disco lleno")}
	r.mustStart(t)
	r.clock.expectWait(t, 5*time.Minute)

	r.clock.expectWait(t, 2*time.Second)

	r.clock.nextWait(t)
	if r.sink.token() != tokenB {
		t.Fatalf("token: got %q", r.sink.token())
	}
	if r.source.count() != 2 {
		t.Fatalf("emisiones: got %d, want 2", r.source.count())
	}
}

func TestRefresher_StartFallaSiNoHayPrimerToken(t *testing.T) {
	denied := fmt.Errorf("%w: sin sesión", application.ErrCloudTokenDenied)
	r := newRig(t, failed(denied), issued(tokenA, start))

	err := r.refresher.Start(context.Background())

	if !errors.Is(err, denied) {
		t.Fatalf("Start: got %v", err)
	}
	if r.sink.writes != 0 {
		t.Fatal("sin token no se escribe nada")
	}
	goleak.VerifyNone(t)
	if err := r.refresher.Start(context.Background()); err != nil {
		t.Fatalf("un Start fallido no deja el renovador iniciado: %v", err)
	}
}

func TestRefresher_StartFallaSiNoSePuedeEscribirElPrimerToken(t *testing.T) {
	r := newRig(t, issued(tokenA, start))
	writeErr := errors.New("disco lleno")
	r.sink.failures = []error{writeErr}

	err := r.refresher.Start(context.Background())

	if !errors.Is(err, writeErr) {
		t.Fatalf("Start: got %v", err)
	}
	assertNoToken(t, err.Error())
	goleak.VerifyNone(t)
}

func TestRefresher_StartDosVecesEsUnError(t *testing.T) {
	r := newRig(t, issued(tokenA, start))
	r.mustStart(t)

	if err := r.refresher.Start(context.Background()); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("segundo Start: got %v", err)
	}
	if r.source.count() != 1 {
		t.Fatal("el segundo Start no debe pedir otro token")
	}
}

func TestRefresher_StopDetieneSinFugasYEsIdempotente(t *testing.T) {
	r := newRig(t, issued(tokenA, start))
	r.refresher.Stop() // sin Start no hace nada
	r.mustStart(t)
	r.clock.nextWait(t)

	r.refresher.Stop()
	r.refresher.Stop()

	goleak.VerifyNone(t)
	r.clock.Advance(time.Hour)
	if r.source.count() != 1 {
		t.Fatal("tras Stop no se renueva")
	}
	if r.sink.token() != tokenA {
		t.Fatal("Stop no borra el token")
	}
}

func TestRefresher_SePuedeVolverAIniciarTrasStop(t *testing.T) {
	r := newRig(t, issued(tokenA, start), issued(tokenB, start), issued(tokenA, start.Add(5*time.Minute)))
	r.mustStart(t)
	r.clock.nextWait(t)
	r.refresher.Stop()

	r.mustStart(t)

	if r.sink.token() != tokenB {
		t.Fatalf("el segundo Start pide un token nuevo: got %q", r.sink.token())
	}
	r.clock.expectWait(t, 5*time.Minute)
	r.clock.nextWait(t)
	if r.source.count() != 3 || r.sink.token() != tokenA {
		t.Fatalf("el segundo ciclo también renueva: %d emisiones, token %q", r.source.count(), r.sink.token())
	}
}

func TestRefresher_UnNuevoStartOlvidaElErrorAnterior(t *testing.T) {
	denied := fmt.Errorf("%w: revocada", application.ErrCloudTokenDenied)
	r := newRig(t, issued(tokenA, start), failed(denied), issued(tokenB, start.Add(5*time.Minute)))
	r.mustStart(t)
	r.clock.expectWait(t, 5*time.Minute)
	waitFor(t, func() bool { return r.refresher.Err() != nil })
	r.refresher.Stop()

	r.mustStart(t)

	if err := r.refresher.Err(); err != nil {
		t.Fatalf("Err tras reiniciar: %v", err)
	}
}

func TestRefresher_CancelarElContextoDetieneLaRenovacion(t *testing.T) {
	r := newRig(t, issued(tokenA, start))
	ctx, cancel := context.WithCancel(context.Background())
	if err := r.refresher.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	r.clock.nextWait(t)

	cancel()

	goleak.VerifyNone(t) // sin llamar a Stop: el ciclo no sobrevive a su contexto
	if err := r.refresher.Err(); err != nil {
		t.Fatalf("una cancelación no es un error definitivo: %v", err)
	}
}

func TestRefresher_StartConElContextoCanceladoNoEscribeNada(t *testing.T) {
	r := newRig(t, issued(tokenA, start))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := r.refresher.Start(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Start: got %v", err)
	}
	if r.sink.writes != 0 {
		t.Fatal("no debe escribirse ningún token")
	}
}

// El uso que hará la sesión: renovador sobre un archivo real, y al terminar (también por una cancelación) no
// queda nada en disco.
func TestRefresher_ConArchivoRealNoDejaElTokenEnDiscoAlCerrar(t *testing.T) {
	for _, name := range []string{"cierre normal", "contexto cancelado"} {
		t.Run(name, func(t *testing.T) {
			defer goleak.VerifyNone(t)
			clock := newFakeClock()
			source := &fakeSource{results: []fetchResult{issued(tokenA, start), issued(tokenB, start.Add(5*time.Minute))}}
			file, err := NewTokenFile(filepath.Join(t.TempDir(), "credenciales"), "run1", TokenFileName)
			if err != nil {
				t.Fatalf("NewTokenFile: %v", err)
			}
			refresher := NewRefresher(source.fetch, file, clock, func(string) {})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			if err := refresher.Start(ctx); err != nil {
				t.Fatalf("Start: %v", err)
			}
			clock.expectWait(t, 5*time.Minute)
			clock.nextWait(t)
			if got, _ := os.ReadFile(file.Path()); string(got) != tokenB {
				t.Fatalf("el archivo no tiene el token renovado: %q", got)
			}

			if name == "contexto cancelado" {
				cancel()
			}
			refresher.Stop()
			if err := file.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}

			if _, err := os.Stat(file.Dir()); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("el directorio del token sigue en disco: %v", err)
			}
		})
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("la condición no se cumplió a tiempo")
		}
		time.Sleep(time.Millisecond)
	}
}
