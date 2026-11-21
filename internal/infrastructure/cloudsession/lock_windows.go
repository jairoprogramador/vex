//go:build windows

package cloudsession

import (
	"errors"
	"io/fs"
	"os"

	"golang.org/x/sys/windows"
)

// tryLock toma, sin esperar, el bloqueo exclusivo de file. Devuelve false si lo tiene otro. El sistema lo suelta
// cuando el proceso que lo tiene termina, como sea que termine.
func tryLock(file *os.File) (bool, error) {
	err := windows.LockFileEx(windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, windows.ERROR_LOCK_VIOLATION), errors.Is(err, windows.ERROR_IO_PENDING):
		return false, nil
	}
	return false, err
}

// En Windows el dueño y los permisos los fija la ACL del directorio de caché del usuario, no un modo.
func ownedByCurrentUser(fs.FileInfo) bool { return true }

func privateMode(fs.FileInfo) bool { return true }
