//go:build unix

package cloudsession

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
)

// tryLock toma, sin esperar, el bloqueo exclusivo de file. Devuelve false si lo tiene otro. El sistema lo suelta
// cuando el proceso que lo tiene termina, como sea que termine.
func tryLock(file *os.File) (bool, error) {
	for {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, syscall.EWOULDBLOCK):
			return false, nil
		case !errors.Is(err, syscall.EINTR):
			return false, err
		}
	}
}

// ownedByCurrentUser dice si el directorio es del usuario que ejecuta vex.
func ownedByCurrentUser(info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Geteuid()
}

// privateMode dice si solo el dueño puede entrar al directorio.
func privateMode(info fs.FileInfo) bool { return info.Mode().Perm() == dirMode }
