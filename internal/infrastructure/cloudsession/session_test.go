package cloudsession

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"go.uber.org/goleak"

	"github.com/jairoprogramador/vex/internal/application"
)

// fakeAdapter es un contrato inventado ("nube/v1"): la sesión no sabe de ninguna nube real.
type fakeAdapter struct {
	err     error
	dropped []string
}

func (a fakeAdapter) Contract() application.ContractID       { return "nube/v1" }
func (a fakeAdapter) Workload() application.WorkloadContract { return a }
func (fakeAdapter) Bootstrap() application.BootstrapProvider { return nil }
func (fakeAdapter) Trust() application.TrustProvider         { return nil }
func (a fakeAdapter) ReservedEnvPrefixes() []string          { return []string{"NUBE_"} }
func (a fakeAdapter) ExecutionContextKey(map[string]string) string {
	return ""
}
func (a fakeAdapter) ErrorHints() []application.ErrorHint { return nil }
func (a fakeAdapter) Env(outputs application.ConnectionOutputs, tokenPath string) (map[string]string, []string, error) {
	if a.err != nil {
		return nil, nil, a.err
	}
	return map[string]string{"NUBE_CUENTA": outputs.Params["cuenta"], "NUBE_TOKEN_FILE": tokenPath}, a.dropped, nil
}

type fakeConnections struct {
	info  application.CloudConnectionInfo
	err   error
	calls int
}

func (f *fakeConnections) CloudConnection(context.Context, string, string) (application.CloudConnectionInfo, error) {
	f.calls++
	return f.info, f.err
}

// fakeTokens adapta fakeSource al puerto y anota para quién se pidió cada token.
type fakeTokens struct {
	source   *fakeSource
	requests []application.TokenRequest
}

func (f *fakeTokens) Issue(ctx context.Context, req application.TokenRequest) (application.IssuedToken, error) {
	f.requests = append(f.requests, req)
	return f.source.fetch(ctx)
}

func activeConnection() application.CloudConnectionInfo {
	return application.CloudConnectionInfo{
		Status: "active", ConnectionID: "c1",
		Outputs: &application.ConnectionOutputs{ContractID: "nube/v1", Params: map[string]string{"cuenta": "123"}},
	}
}

type sessionRig struct {
	base     string
	clock    *fakeClock
	conns    *fakeConnections
	tokens   *fakeTokens
	warnings *warnings
	creds    *Credentials
}

func newSessionRig(t *testing.T, adapter fakeAdapter, results ...fetchResult) *sessionRig {
	t.Helper()
	r := &sessionRig{
		base:     filepath.Join(t.TempDir(), "credenciales"),
		clock:    newFakeClock(),
		conns:    &fakeConnections{info: activeConnection()},
		tokens:   &fakeTokens{source: &fakeSource{results: results}},
		warnings: &warnings{},
	}
	r.creds = NewCredentials(r.conns, r.tokens, application.NewCloudAdapters(adapter), r.base, r.warnings.add).WithClock(r.clock)
	return r
}

var sessionRequest = application.CloudSessionRequest{
	ProjectID: "p1", Environment: "sand", Commit: "abc", CloudSteps: []string{"supply", "package", "deploy"},
}

func (r *sessionRig) open(t *testing.T) *Session {
	t.Helper()
	session, err := r.creds.Open(context.Background(), sessionRequest)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return session.(*Session)
}

func (r *sessionRig) hostTokenPath(s *Session) string {
	return filepath.Join(s.file.Dir(), TokenFileName)
}

func readToken(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestOpen_PreparaElContratoSinPedirNingunToken(t *testing.T) {
	r := newSessionRig(t, fakeAdapter{}, issued(tokenA, start))
	session := r.open(t)
	defer session.Close()

	if r.conns.calls != 1 {
		t.Fatalf("consultas de la conexión: %d", r.conns.calls)
	}
	if r.tokens.source.count() != 0 {
		t.Fatal("abrir la sesión no debe emitir ningún token")
	}

	env := session.Env()
	want := map[string]string{"NUBE_CUENTA": "123", "NUBE_TOKEN_FILE": "/vex/credenciales/oidc.jwt", "VEX_CLOUD_CONTRACT": "nube/v1"}
	if fmt.Sprint(env) != fmt.Sprint(want) {
		t.Fatalf("env: got %v, want %v", env, want)
	}
	env["NUBE_CUENTA"] = "otra"
	if session.Env()["NUBE_CUENTA"] != "123" {
		t.Fatal("Env debe devolver una copia")
	}

	mounts := session.Mounts()
	if len(mounts) != 1 || mounts[0].Target != "/vex/credenciales" || !mounts[0].ReadOnly {
		t.Fatalf("mounts: %+v", mounts)
	}
	dir := mounts[0].Source
	if filepath.Dir(dir) != r.base {
		t.Fatalf("el directorio %q debe colgar de %q", dir, r.base)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("modo del directorio: %v", info.Mode().Perm())
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("el directorio debe estar vacío hasta el primer paso de nube: %v %v", entries, err)
	}
}

func TestOpen_ConLaConexionDeLaPeticionNoVuelveAConsultar(t *testing.T) {
	r := newSessionRig(t, fakeAdapter{})
	conn := activeConnection()
	req := sessionRequest
	req.Connection = &conn

	session, err := r.creds.Open(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	if r.conns.calls != 0 {
		t.Fatalf("no debe consultar otra vez: %d", r.conns.calls)
	}
}

func TestOpen_SinConexionActivaNoHaySesion(t *testing.T) {
	for _, status := range []string{"absent", "pending", "verifying", "failed", "revoked"} {
		t.Run(status, func(t *testing.T) {
			r := newSessionRig(t, fakeAdapter{})
			r.conns.info = application.CloudConnectionInfo{Status: status, Hint: "vex bootstrap sand"}

			_, err := r.creds.Open(context.Background(), sessionRequest)

			var notReady *application.ConnectionNotReadyError
			if !errors.As(err, &notReady) || notReady.Status != status {
				t.Fatalf("got %v", err)
			}
			if !strings.Contains(err.Error(), "Ejecuta `vex bootstrap sand`") {
				t.Fatalf("el mensaje debe decir qué ejecutar: %v", err)
			}
			if _, statErr := os.Stat(r.base); !errors.Is(statErr, fs.ErrNotExist) {
				entries, _ := os.ReadDir(r.base)
				if len(entries) != 0 {
					t.Fatalf("no debe quedar ningún directorio: %v", entries)
				}
			}
		})
	}
}

func TestOpen_Errores(t *testing.T) {
	t.Run("la consulta falla", func(t *testing.T) {
		r := newSessionRig(t, fakeAdapter{})
		r.conns.err = application.ErrCloudTokenDenied

		_, err := r.creds.Open(context.Background(), sessionRequest)

		if !errors.Is(err, application.ErrCloudTokenDenied) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("contrato sin adaptador", func(t *testing.T) {
		r := newSessionRig(t, fakeAdapter{})
		r.conns.info.Outputs.ContractID = "otra/v1"

		_, err := r.creds.Open(context.Background(), sessionRequest)

		var unsupported *application.UnsupportedContractError
		if !errors.As(err, &unsupported) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("la conexión no cumple el contrato", func(t *testing.T) {
		r := newSessionRig(t, fakeAdapter{err: errors.New("falta la cuenta")})

		_, err := r.creds.Open(context.Background(), sessionRequest)

		if err == nil || !strings.Contains(err.Error(), "falta la cuenta") || !strings.Contains(err.Error(), "vex bootstrap sand") {
			t.Fatalf("got %v", err)
		}
		if entries, _ := os.ReadDir(r.base); len(entries) != 0 {
			t.Fatalf("no debe quedar ningún directorio: %v", entries)
		}
	})
	t.Run("datos desconocidos solo se avisan", func(t *testing.T) {
		r := newSessionRig(t, fakeAdapter{dropped: []string{"nuevo_dato"}})

		defer r.open(t).Close()

		if all := r.warnings.all(); len(all) != 1 || !strings.Contains(all[0], "nuevo_dato") {
			t.Fatalf("avisos: %v", all)
		}
	})
}

func TestSession_ElTokenSoloExisteDuranteLosPasosDeNube(t *testing.T) {
	defer goleak.VerifyNone(t)
	r := newSessionRig(t, fakeAdapter{}, issued(tokenA, start), issued(tokenB, start), issued("tercero", start))
	session := r.open(t)
	defer session.Close()
	path := r.hostTokenPath(session)
	ctx := context.Background()

	if err := session.StepRunning(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	if got := readToken(t, path); got != "" || r.tokens.source.count() != 0 {
		t.Fatalf("un paso que no es de nube no recibe token: %q", got)
	}
	if err := session.StepFinished("test"); err != nil {
		t.Fatal(err)
	}

	if err := session.StepRunning(ctx, "supply"); err != nil {
		t.Fatal(err)
	}
	if got := readToken(t, path); got != tokenA {
		t.Fatal("al volver de StepRunning el primer token ya debe estar escrito")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("modo del token: %v %v", info, err)
	}
	// Los eventos de los comandos del mismo paso no emiten más tokens.
	for range 3 {
		if err := session.StepRunning(ctx, "supply"); err != nil {
			t.Fatal(err)
		}
	}
	if r.tokens.source.count() != 1 {
		t.Fatalf("emisiones durante supply: %d", r.tokens.source.count())
	}
	want := application.TokenRequest{ProjectID: "p1", Environment: "sand", Commit: "abc"}
	if r.tokens.requests[0] != want {
		t.Fatalf("petición del token: %+v", r.tokens.requests[0])
	}

	if err := session.StepFinished("supply"); err != nil {
		t.Fatal(err)
	}
	if got := readToken(t, path); got != "" {
		t.Fatal("al terminar el paso el token ya no debe existir")
	}
	if _, err := os.Stat(session.file.Dir()); err != nil {
		t.Fatalf("el directorio sigue montado hasta cerrar: %v", err)
	}

	// Dos pasos de nube seguidos: dos ciclos, dos emisiones.
	if err := session.StepRunning(ctx, "package"); err != nil {
		t.Fatal(err)
	}
	if got := readToken(t, path); got != tokenB || r.tokens.source.count() != 2 {
		t.Fatalf("package: token %q, emisiones %d", got, r.tokens.source.count())
	}
	if err := session.StepFinished("package"); err != nil {
		t.Fatal(err)
	}
	if got := readToken(t, path); got != "" {
		t.Fatal("el token de package debe retirarse")
	}
}

func TestSession_RenuevaElTokenMientrasDuraElPaso(t *testing.T) {
	defer goleak.VerifyNone(t)
	r := newSessionRig(t, fakeAdapter{}, issued(tokenA, start), issued(tokenB, start.Add(5*60e9)))
	session := r.open(t)
	defer session.Close()

	if err := session.StepRunning(context.Background(), "supply"); err != nil {
		t.Fatal(err)
	}
	r.clock.expectWait(t, 5*60e9)
	r.clock.nextWait(t) // renovó y volvió a dormir

	if got := readToken(t, r.hostTokenPath(session)); got != tokenB {
		t.Fatal("el token debe renovarse durante el paso")
	}
}

// Los tres casos siguientes son el motor perdiendo eventos (su cola de progreso descarta si nadie lee a tiempo).

func TestSession_SiSePierdeElFinalDeUnPasoElSiguienteRetiraElToken(t *testing.T) {
	defer goleak.VerifyNone(t)
	r := newSessionRig(t, fakeAdapter{}, issued(tokenA, start))
	session := r.open(t)
	defer session.Close()
	ctx := context.Background()

	if err := session.StepRunning(ctx, "supply"); err != nil {
		t.Fatal(err)
	}
	// Nunca llega StepFinished("supply"): lo siguiente que se sabe es que corre un paso que no es de nube.
	if err := session.StepRunning(ctx, "smoke"); err != nil {
		t.Fatal(err)
	}

	if got := readToken(t, r.hostTokenPath(session)); got != "" {
		t.Fatal("un paso que no es de nube no puede ver el token del anterior")
	}
}

func TestSession_SiSePierdeElComienzoDeUnPasoSeRecuperaConSuSiguienteEvento(t *testing.T) {
	defer goleak.VerifyNone(t)
	r := newSessionRig(t, fakeAdapter{}, issued(tokenA, start))
	session := r.open(t)
	defer session.Close()

	// No llegó paso_iniciado de deploy; llega el final de su primer comando, que también llama a StepRunning.
	if err := session.StepRunning(context.Background(), "deploy"); err != nil {
		t.Fatal(err)
	}

	if got := readToken(t, r.hostTokenPath(session)); got != tokenA {
		t.Fatal("el paso de nube debe recuperar su token")
	}
}

func TestSession_UnFinalSinComienzoNoHaceNada(t *testing.T) {
	r := newSessionRig(t, fakeAdapter{}, issued(tokenA, start))
	session := r.open(t)
	defer session.Close()

	// Un paso reutilizado solo anuncia que terminó: no recibe token.
	if err := session.StepFinished("supply"); err != nil {
		t.Fatal(err)
	}

	if r.tokens.source.count() != 0 {
		t.Fatal("un paso reutilizado no emite token")
	}
}

func TestSession_CloseBorraTodoAunAMitadDeUnPaso(t *testing.T) {
	defer goleak.VerifyNone(t)
	r := newSessionRig(t, fakeAdapter{}, issued(tokenA, start))
	session := r.open(t)
	defer session.Close()
	if err := session.StepRunning(context.Background(), "supply"); err != nil {
		t.Fatal(err)
	}
	dir := session.file.Dir()

	if err := session.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("el directorio debe desaparecer: %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close es idempotente: %v", err)
	}
	// Un evento que llega tarde no resucita el token.
	if err := session.StepRunning(context.Background(), "deploy"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) || r.tokens.source.count() != 1 {
		t.Fatal("tras cerrar no se emite ni se escribe nada")
	}
}

func TestSession_SiNoSePuedeObtenerElTokenNoQuedaNada(t *testing.T) {
	defer goleak.VerifyNone(t)
	r := newSessionRig(t, fakeAdapter{}, failed(errTransient), issued(tokenA, start))
	session := r.open(t)
	defer session.Close()
	ctx := context.Background()

	err := session.StepRunning(ctx, "supply")

	if !errors.Is(err, errTransient) {
		t.Fatalf("got %v", err)
	}
	if got := readToken(t, r.hostTokenPath(session)); got != "" {
		t.Fatal("sin token no debe quedar archivo")
	}
	// El siguiente evento del mismo paso lo vuelve a intentar.
	if err := session.StepRunning(ctx, "supply"); err != nil {
		t.Fatal(err)
	}
	if got := readToken(t, r.hostTokenPath(session)); got != tokenA {
		t.Fatal("el reintento debe dejar el token")
	}
}

func TestSession_UnRechazoAlRenovarSeSabeEnElSiguienteEvento(t *testing.T) {
	defer goleak.VerifyNone(t)
	denied := fmt.Errorf("%w: %w", application.ErrCloudTokenDenied, application.ErrCloudConnectionInactive)
	r := newSessionRig(t, fakeAdapter{}, issued(tokenA, start), failed(denied))
	session := r.open(t)
	defer session.Close()
	ctx := context.Background()

	if err := session.StepRunning(ctx, "supply"); err != nil {
		t.Fatal(err)
	}
	r.clock.expectWait(t, 5*60e9)
	waitFor(t, func() bool { return session.refresher.Err() != nil })

	err := session.StepRunning(ctx, "supply")

	if !errors.Is(err, application.ErrCloudConnectionInactive) {
		t.Fatalf("got %v", err)
	}
}

var jwtShape = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.`)

func TestSession_ElTokenNoApareceEnElEntornoNiEnLosAvisos(t *testing.T) {
	r := newSessionRig(t, fakeAdapter{}, issued(tokenA, start))
	session := r.open(t)
	defer session.Close()
	if err := session.StepRunning(context.Background(), "supply"); err != nil {
		t.Fatal(err)
	}

	for name, value := range session.Env() {
		if strings.Contains(value, tokenA) || jwtShape.MatchString(value) {
			t.Fatalf("la variable %s lleva el token", name)
		}
	}
	for _, mount := range session.Mounts() {
		if strings.Contains(mount.Source+mount.Target, tokenA) {
			t.Fatal("el montaje lleva el token")
		}
	}
	for _, warning := range r.warnings.all() {
		if strings.Contains(warning, tokenA) {
			t.Fatal("un aviso lleva el token")
		}
	}
}
