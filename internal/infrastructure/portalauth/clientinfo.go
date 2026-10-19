package portalauth

import (
	"os"
	"runtime"
)

var hostnameFn = os.Hostname // sustituible en pruebas

// clientInfo es lo que la CLI declara de sí misma; el servidor añade IP y país por su cuenta.
func clientInfo() (osName, host string) {
	osName = runtime.GOOS + "/" + runtime.GOARCH
	if h, err := hostnameFn(); err == nil {
		host = h
	}
	return
}
