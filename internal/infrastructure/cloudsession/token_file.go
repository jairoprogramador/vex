package cloudsession

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/jairoprogramador/vex/internal/application"
)

// TokenFileName es el nombre del archivo del token. Es el mismo para cualquier nube: cada adaptador lo expone
// con las variables de la suya.
const TokenFileName = application.TokenFileName

// En Windows los modos no se aplican: el directorio de caché del usuario ya es privado por ACL.
const (
	dirMode  fs.FileMode = 0o700
	fileMode fs.FileMode = 0o600
	// lockSuffix nombra el archivo de bloqueo de una ejecución: <id>.lock, junto a su directorio y no dentro,
	// porque el directorio se monta en el contenedor y ahí solo va el token.
	lockSuffix = ".lock"
)

// TokenFile es el archivo del token de una ejecución, solo en su directorio. Lo que se monta en el contenedor
// es el directorio, no el archivo: cada escritura reemplaza el archivo y un montaje del archivo seguiría viendo
// el anterior.
type TokenFile struct {
	dir  string
	name string
	// lock es el archivo de bloqueo del directorio, tomado mientras la ejecución vive: así SweepStale de otra
	// ejecución distingue un directorio vivo y callado (un paso largo sin renovar) de uno abandonado.
	lock *os.File
	// mu serializa las operaciones: una escritura tardía no puede cruzarse con el borrado.
	mu sync.Mutex
}

// DefaultBaseDir es donde viven los directorios de credenciales: <caché del usuario>/vex/credenciales.
func DefaultBaseDir() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("ubicar el directorio de credenciales: %w", err)
	}
	return filepath.Join(cache, "vex", "credenciales"), nil
}

// NewRunID devuelve un identificador aleatorio para el directorio de una ejecución.
func NewRunID() (string, error) {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generar el id de ejecución: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// NewTokenFile crea el directorio privado baseDir/runID donde vivirá el archivo name. El directorio tiene que
// ser nuevo: si ya existe no es de esta ejecución y no se reutiliza.
func NewTokenFile(baseDir, runID, name string) (*TokenFile, error) {
	if !isPathElement(runID) || !isPathElement(name) {
		return nil, fmt.Errorf("directorio de credenciales: %q y %q deben ser nombres simples", runID, name)
	}
	if err := os.MkdirAll(baseDir, dirMode); err != nil {
		return nil, fmt.Errorf("crear el directorio de credenciales: %w", err)
	}
	if err := checkBaseDir(baseDir); err != nil {
		return nil, err
	}
	dir := filepath.Join(baseDir, runID)
	// El bloqueo se toma antes de crear el directorio: nunca hay un directorio vivo sin bloqueo.
	lock, err := holdLock(dir)
	if err != nil {
		return nil, fmt.Errorf("bloquear el directorio de credenciales: %w", err)
	}
	release := func() {
		_ = lock.Close()
		_ = os.Remove(dir + lockSuffix)
	}
	if err := os.Mkdir(dir, dirMode); err != nil {
		release()
		return nil, fmt.Errorf("crear el directorio de credenciales: %w", err)
	}
	// Mkdir aplica la umask, que solo quita permisos; Chmod deja exactamente los que se quieren.
	if err := os.Chmod(dir, dirMode); err != nil {
		_ = os.Remove(dir)
		release()
		return nil, fmt.Errorf("proteger el directorio de credenciales: %w", err)
	}
	return &TokenFile{dir: dir, name: name, lock: lock}, nil
}

// checkBaseDir comprueba que el directorio donde se dejarán tokens es de verdad privado: un directorio (no un
// enlace que lleve a otra parte), del usuario, y sin acceso para nadie más. Si ya existía, pudo crearlo otro.
func checkBaseDir(baseDir string) error {
	info, err := os.Lstat(baseDir)
	if err != nil {
		return fmt.Errorf("revisar el directorio de credenciales: %w", err)
	}
	var problem string
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		problem = "es un enlace simbólico"
	case !info.IsDir():
		problem = "no es un directorio"
	case !ownedByCurrentUser(info):
		problem = "no es de tu usuario"
	case !privateMode(info):
		problem = fmt.Sprintf("tiene permisos %04o y deberían ser %04o", info.Mode().Perm(), dirMode)
	default:
		return nil
	}
	return fmt.Errorf("el directorio de credenciales %s %s: no se dejan tokens ahí. Si no lo cambiaste tú, "+
		"averigua quién; para empezar de cero, bórralo y vuelve a ejecutar", baseDir, problem)
}

// holdLock crea el archivo de bloqueo de dir y lo deja tomado.
func holdLock(dir string) (*os.File, error) {
	file, err := os.OpenFile(dir+lockSuffix, os.O_RDWR|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		return nil, err
	}
	if locked, err := tryLock(file); err != nil || !locked {
		_ = file.Close()
		_ = os.Remove(dir + lockSuffix)
		if err == nil {
			err = errors.New("lo tiene otro proceso")
		}
		return nil, err
	}
	return file, nil
}

// inUse dice si dir es de una ejecución viva: su archivo de bloqueo sigue tomado. Un directorio sin archivo de
// bloqueo no es de nadie.
func inUse(dir string) (bool, error) {
	file, err := os.OpenFile(dir+lockSuffix, os.O_RDWR, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer file.Close()
	locked, err := tryLock(file)
	return !locked, err
}

func isPathElement(name string) bool {
	return name != "" && name != "." && name != ".." && name == filepath.Base(name)
}

// Dir es el directorio que se monta en el contenedor.
func (f *TokenFile) Dir() string { return f.dir }

// Path es la ruta del archivo del token en el anfitrión.
func (f *TokenFile) Path() string { return filepath.Join(f.dir, f.name) }

func (f *TokenFile) tempPath() string { return filepath.Join(f.dir, "."+f.name+".tmp") }

// Write deja token como contenido del archivo de forma atómica: quien lo lea ve el token anterior o el nuevo,
// nunca uno a medias. Sus errores no incluyen el token.
func (f *TokenFile) Write(token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	tmp := f.tempPath()
	if err := writeNewFile(tmp, token); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("escribir el token de nube: %w", err)
	}
	if err := os.Rename(tmp, f.Path()); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("publicar el token de nube: %w", err)
	}
	if err := syncDir(f.dir); err != nil {
		return fmt.Errorf("asentar el token de nube: %w", err)
	}
	return nil
}

// writeNewFile crea path (que no debe existir) con contenido content y lo asienta en disco.
func writeNewFile(path, content string) error {
	// Un temporal que quedó de una escritura interrumpida no debe impedir las siguientes.
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		return err
	}
	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil // Windows no permite sincronizar un directorio.
	}
	handle, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := handle.Sync(); err != nil {
		_ = handle.Close()
		return err
	}
	return handle.Close()
}

// Clear borra el archivo del token y deja el directorio: el token deja de estar disponible hasta la próxima
// escritura. Es idempotente.
func (f *TokenFile) Clear() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, path := range []string{f.Path(), f.tempPath()} {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("borrar el token de nube: %w", err)
		}
	}
	return nil
}

// Close borra el directorio entero. Es idempotente; después de Close, Write falla.
func (f *TokenFile) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// El directorio primero y el bloqueo después: mientras quede algo que borrar, sigue siendo de esta ejecución.
	if err := os.RemoveAll(f.dir); err != nil {
		return fmt.Errorf("borrar el directorio de credenciales: %w", err)
	}
	if f.lock != nil {
		_ = f.lock.Close()
		f.lock = nil
		_ = os.Remove(f.dir + lockSuffix)
	}
	return nil
}

// SweepStale borra los directorios de ejecuciones que no cerraron (un kill -9, un corte de luz): los que hay en
// baseDir sin cambios desde hace más de olderThan y cuyo archivo de bloqueo nadie tiene. Una ejecución viva lo
// tiene tomado hasta que termina, aunque lleve horas sin renovar el token; si el proceso muere, el sistema lo
// suelta. Ante la duda (no se puede saber si está en uso) no se borra.
func SweepStale(baseDir string, olderThan time.Duration, now time.Time) error {
	entries, err := os.ReadDir(baseDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("revisar el directorio de credenciales: %w", err)
	}

	limit := now.Add(-olderThan)
	var failures []error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue // lo borró otra ejecución mientras se revisaba
		}
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if !info.ModTime().Before(limit) {
			continue
		}
		dir := filepath.Join(baseDir, entry.Name())
		if live, err := inUse(dir); err != nil {
			failures = append(failures, err)
			continue
		} else if live {
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			failures = append(failures, err)
			continue
		}
		if err := os.Remove(dir + lockSuffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
			failures = append(failures, err)
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("limpiar credenciales antiguas: %w", errors.Join(failures...))
	}
	return nil
}
