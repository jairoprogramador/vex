package cloudsession

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// Tokens reconocibles: si alguno aparece en un error o en un aviso, el test lo ve.
const (
	tokenA = "eyJhbGciOiJSUzI1NiJ9.dG9rZW4tQQ.ZmlybWEtQQ"
	tokenB = "eyJhbGciOiJSUzI1NiJ9.dG9rZW4tQg.ZmlybWEtQg"
)

func newTokenFile(t *testing.T) *TokenFile {
	t.Helper()
	file, err := NewTokenFile(filepath.Join(t.TempDir(), "credenciales"), "run1", TokenFileName)
	if err != nil {
		t.Fatalf("NewTokenFile: %v", err)
	}
	return file
}

func mode(t *testing.T, path string) fs.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Mode().Perm()
}

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("los modos de archivo no se aplican en Windows")
	}
}

func TestNewTokenFile_RutasYNombreNeutral(t *testing.T) {
	base := filepath.Join(t.TempDir(), "credenciales")

	file, err := NewTokenFile(base, "run1", TokenFileName)
	if err != nil {
		t.Fatalf("NewTokenFile: %v", err)
	}

	if file.Dir() != filepath.Join(base, "run1") {
		t.Fatalf("Dir: got %q", file.Dir())
	}
	if file.Path() != filepath.Join(base, "run1", "oidc.jwt") {
		t.Fatalf("Path: got %q", file.Path())
	}
	if _, err := os.Stat(file.Path()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("el archivo no debe existir hasta la primera escritura: %v", err)
	}
}

func TestTokenFile_PermisosMinimos(t *testing.T) {
	skipOnWindows(t)
	file := newTokenFile(t)

	if err := file.Write(tokenA); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := file.Write(tokenB); err != nil {
		t.Fatalf("segundo Write: %v", err)
	}

	if got := mode(t, file.Dir()); got != 0o700 {
		t.Fatalf("directorio: got %o, want 700", got)
	}
	if got := mode(t, file.Path()); got != 0o600 {
		t.Fatalf("archivo: got %o, want 600", got)
	}
}

// privateBase es un directorio base como el que crea vex: solo del usuario.
func privateBase(t *testing.T) string {
	t.Helper()
	base := filepath.Join(t.TempDir(), "credenciales")
	if err := os.Mkdir(base, 0o700); err != nil {
		t.Fatal(err)
	}
	return base
}

func TestNewTokenFile_NoReutilizaUnDirectorioAjeno(t *testing.T) {
	base := privateBase(t)
	if err := os.Mkdir(filepath.Join(base, "run1"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := NewTokenFile(base, "run1", TokenFileName); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("un directorio que ya existe no es de esta ejecución: got %v", err)
	}
}

func TestNewTokenFile_RechazaNombresQueSalenDelDirectorio(t *testing.T) {
	base := t.TempDir()
	for _, bad := range []string{"", ".", "..", "../fuera", "a/b"} {
		if _, err := NewTokenFile(base, bad, TokenFileName); err == nil {
			t.Fatalf("runID %q debería rechazarse", bad)
		}
		if _, err := NewTokenFile(base, "run-"+strings.ReplaceAll(bad, "/", "_"), bad); err == nil {
			t.Fatalf("name %q debería rechazarse", bad)
		}
	}
}

func TestTokenFile_WriteReemplazaElContenidoSinDejarTemporales(t *testing.T) {
	file := newTokenFile(t)

	for _, token := range []string{tokenA, tokenB} {
		if err := file.Write(token); err != nil {
			t.Fatalf("Write: %v", err)
		}
		got, err := os.ReadFile(file.Path())
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != token {
			t.Fatalf("contenido: got %q", got)
		}
	}

	entries, err := os.ReadDir(file.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != TokenFileName {
		t.Fatalf("en el directorio solo debe quedar el token: %v", entries)
	}
}

func TestTokenFile_UnLectorNuncaVeContenidoParcial(t *testing.T) {
	file := newTokenFile(t)
	// Dos contenidos largos y distintos: una lectura a medias no sería ninguno de los dos.
	long := map[string]bool{
		strings.Repeat("a", 64*1024): true,
		strings.Repeat("b", 64*1024): true,
	}
	contents := make([]string, 0, len(long))
	for content := range long {
		contents = append(contents, content)
	}
	if err := file.Write(contents[0]); err != nil {
		t.Fatalf("Write: %v", err)
	}

	stop := make(chan struct{})
	var writer sync.WaitGroup
	writer.Add(1)
	go func() {
		defer writer.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if err := file.Write(contents[i%2]); err != nil {
				t.Errorf("Write: %v", err)
				return
			}
		}
	}()

	for i := 0; i < 100; i++ {
		got, err := os.ReadFile(file.Path())
		if err != nil {
			t.Errorf("lectura %d: %v", i, err)
			break
		}
		if !long[string(got)] {
			t.Errorf("lectura %d: contenido parcial de %d bytes", i, len(got))
			break
		}
	}
	close(stop)
	writer.Wait()
}

func TestTokenFile_UnTemporalHuerfanoNoImpideEscribir(t *testing.T) {
	file := newTokenFile(t)
	if err := os.WriteFile(file.tempPath(), []byte("a medias"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := file.Write(tokenA); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got, _ := os.ReadFile(file.Path()); string(got) != tokenA {
		t.Fatalf("contenido: got %q", got)
	}
}

func TestTokenFile_SiFallaNoDejaTemporalNiFiltraElToken(t *testing.T) {
	file := newTokenFile(t)
	if err := file.Write(tokenA); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// Un directorio donde debería ir el archivo hace fallar el rename, después de escribir el temporal.
	if err := os.Remove(file.Path()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(file.Path(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(file.Path(), "ocupado"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	err := file.Write(tokenB)

	if err == nil {
		t.Fatal("se esperaba un error")
	}
	assertNoToken(t, err.Error())
	if _, statErr := os.Stat(file.tempPath()); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("el temporal quedó en disco: %v", statErr)
	}
}

func TestTokenFile_ClearEsIdempotenteYDejaElDirectorio(t *testing.T) {
	file := newTokenFile(t)
	if err := file.Write(tokenA); err != nil {
		t.Fatalf("Write: %v", err)
	}

	for i := 0; i < 2; i++ {
		if err := file.Clear(); err != nil {
			t.Fatalf("Clear %d: %v", i, err)
		}
	}

	if _, err := os.Stat(file.Path()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("el token sigue en disco: %v", err)
	}
	if _, err := os.Stat(file.Dir()); err != nil {
		t.Fatalf("Clear no debe borrar el directorio: %v", err)
	}
	if err := file.Write(tokenB); err != nil {
		t.Fatalf("tras Clear se puede volver a escribir: %v", err)
	}
}

func TestTokenFile_CloseBorraTodoYEsIdempotente(t *testing.T) {
	file := newTokenFile(t)
	if err := file.Write(tokenA); err != nil {
		t.Fatalf("Write: %v", err)
	}

	for i := 0; i < 2; i++ {
		if err := file.Close(); err != nil {
			t.Fatalf("Close %d: %v", i, err)
		}
	}

	if _, err := os.Stat(file.Dir()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("el directorio sigue en disco: %v", err)
	}
	if err := file.Clear(); err != nil {
		t.Fatalf("Clear tras Close: %v", err)
	}
}

func TestTokenFile_UnaEscrituraTardiaNoResucitaElToken(t *testing.T) {
	file := newTokenFile(t)
	if err := file.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	err := file.Write(tokenA)

	if err == nil {
		t.Fatal("escribir tras Close debe fallar")
	}
	assertNoToken(t, err.Error())
	if _, statErr := os.Stat(file.Dir()); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("el directorio reapareció: %v", statErr)
	}
}

func TestSweepStale_BorraSoloLoAntiguo(t *testing.T) {
	base := t.TempDir()
	now := time.Now()
	makeDir := func(name string, age time.Duration) string {
		dir := filepath.Join(base, name)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, TokenFileName), []byte(tokenA), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(dir, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	old := makeDir("viejo", 2*time.Hour)
	recent := makeDir("reciente", 10*time.Minute)
	stray := filepath.Join(base, "suelto.txt")
	if err := os.WriteFile(stray, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(stray, now.Add(-48*time.Hour), now.Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}

	if err := SweepStale(base, time.Hour, now); err != nil {
		t.Fatalf("SweepStale: %v", err)
	}

	if _, err := os.Stat(old); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("el directorio antiguo sigue ahí: %v", err)
	}
	if _, err := os.Stat(filepath.Join(recent, TokenFileName)); err != nil {
		t.Fatalf("el directorio reciente no se toca: %v", err)
	}
	if _, err := os.Stat(stray); err != nil {
		t.Fatalf("lo que no es un directorio de ejecución no se toca: %v", err)
	}
}

func TestSweepStale_SinDirectorioBaseNoEsUnError(t *testing.T) {
	if err := SweepStale(filepath.Join(t.TempDir(), "no-existe"), time.Hour, time.Now()); err != nil {
		t.Fatalf("SweepStale: %v", err)
	}
}

func TestNewRunID_AleatorioYConFormato(t *testing.T) {
	pattern := regexp.MustCompile(`^[0-9a-f]{16}$`)
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id, err := NewRunID()
		if err != nil {
			t.Fatalf("NewRunID: %v", err)
		}
		if !pattern.MatchString(id) || seen[id] {
			t.Fatalf("id inválido o repetido: %q", id)
		}
		seen[id] = true
	}
}

func TestDefaultBaseDir_CuelgaDeLaCacheDelUsuario(t *testing.T) {
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Skipf("sin directorio de caché: %v", err)
	}
	got, err := DefaultBaseDir()
	if err != nil {
		t.Fatalf("DefaultBaseDir: %v", err)
	}
	if got != filepath.Join(cache, "vex", "credenciales") {
		t.Fatalf("got %q", got)
	}
}

func assertNoToken(t *testing.T, text string) {
	t.Helper()
	for _, secret := range []string{"eyJ", "dG9rZW4tQQ", "dG9rZW4tQg", "ZmlybWEt"} {
		if strings.Contains(text, secret) {
			t.Fatalf("el texto reproduce el token (%q): %s", secret, text)
		}
	}
}

// B10 de la revisión de seguridad: una ejecución viva no pierde su directorio aunque lleve horas sin tocarlo.
func TestSweepStale_NoBorraElDirectorioDeUnaEjecucionViva(t *testing.T) {
	base := privateBase(t)
	file, err := NewTokenFile(base, "viva", TokenFileName)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Write(tokenA); err != nil {
		t.Fatal(err)
	}
	// Un paso largo: nadie renovó el token en tres horas.
	old := time.Now().Add(-3 * time.Hour)
	if err := os.Chtimes(file.Dir(), old, old); err != nil {
		t.Fatal(err)
	}

	if err := SweepStale(base, time.Hour, time.Now()); err != nil {
		t.Fatalf("SweepStale: %v", err)
	}
	if got, err := os.ReadFile(file.Path()); err != nil || string(got) != tokenA {
		t.Fatalf("el token de la ejecución viva desapareció: %q, %v", got, err)
	}

	// Cuando termina sin limpiar (aquí, soltando el bloqueo como lo haría la muerte del proceso), sí se borra.
	if err := file.lock.Close(); err != nil {
		t.Fatal(err)
	}
	file.lock = nil
	if err := os.Chtimes(file.Dir(), old, old); err != nil {
		t.Fatal(err)
	}
	if err := SweepStale(base, time.Hour, time.Now()); err != nil {
		t.Fatalf("SweepStale: %v", err)
	}
	if entries, err := os.ReadDir(base); err != nil || len(entries) != 0 {
		t.Fatalf("lo abandonado sigue ahí: %v, %v", entries, err)
	}
}

func TestTokenFile_CloseSueltaElBloqueoYBorraTodo(t *testing.T) {
	base := privateBase(t)
	file, err := NewTokenFile(base, "run", TokenFileName)
	if err != nil {
		t.Fatal(err)
	}
	if live, err := inUse(file.Dir()); err != nil || !live {
		t.Fatalf("en uso = %v, %v; una ejecución abierta tiene su bloqueo", live, err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close dos veces: %v", err)
	}
	if entries, err := os.ReadDir(base); err != nil || len(entries) != 0 {
		t.Fatalf("queda algo de la ejecución: %v, %v", entries, err)
	}
}

func TestNewTokenFile_ElDirectorioBaseTieneQueSerPrivado(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("en Windows lo protege la ACL")
	}
	t.Run("abierto a otros", func(t *testing.T) {
		base := filepath.Join(t.TempDir(), "credenciales")
		if err := os.Mkdir(base, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(base, 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := NewTokenFile(base, "run", TokenFileName)
		if err == nil || !strings.Contains(err.Error(), "permisos 0755") {
			t.Fatalf("error = %v", err)
		}
		if entries, _ := os.ReadDir(base); len(entries) != 0 {
			t.Errorf("no debe crear nada dentro: %v", entries)
		}
	})
	t.Run("es un enlace", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "otro-sitio")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
		base := filepath.Join(root, "credenciales")
		if err := os.Symlink(target, base); err != nil {
			t.Fatal(err)
		}
		_, err := NewTokenFile(base, "run", TokenFileName)
		if err == nil || !strings.Contains(err.Error(), "enlace simbólico") {
			t.Fatalf("error = %v", err)
		}
		if entries, _ := os.ReadDir(target); len(entries) != 0 {
			t.Errorf("no debe escribir a través del enlace: %v", entries)
		}
	})
	t.Run("es un archivo", func(t *testing.T) {
		base := filepath.Join(t.TempDir(), "credenciales")
		if err := os.WriteFile(base, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := NewTokenFile(base, "run", TokenFileName); err == nil {
			t.Fatal("se esperaba un error")
		}
	})
	t.Run("recién creado vale", func(t *testing.T) {
		file, err := NewTokenFile(filepath.Join(t.TempDir(), "a", "credenciales"), "run", TokenFileName)
		if err != nil {
			t.Fatal(err)
		}
		_ = file.Close()
	})
}
