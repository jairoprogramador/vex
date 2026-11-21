package engine

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// containerHome es el HOME que la imagen de runtime garantiza escribible por cualquier UID (contrato de imagen).
const containerHome = "/home/vex"

// platform es lo que hay que saber del anfitrión para decidir con qué usuario corre el contenedor.
type platform struct {
	goos     string
	uid, gid func() int
	// rootless dice si Docker corre sin privilegios; solo se consulta en Linux.
	rootless func() bool
}

// hostRunUser devuelve los argumentos de `docker run` que hacen que el contenedor use al usuario del anfitrión,
// o nil si no hace falta.
//
// En Linux un montaje conserva dueño y permisos del anfitrión: el directorio del token es 0700 del usuario y
// un contenedor con otro UID no podría leerlo (ni escribir en los directorios de trabajo). En vez de relajar
// esos permisos, el contenedor corre con el UID del usuario. Con Docker rootless el usuario del anfitrión es
// el 0 del contenedor. En macOS y Windows Docker Desktop ya hace esa traducción y no se toca nada.
func hostRunUser(p platform) []string {
	if p.goos != "linux" {
		return nil
	}
	user := fmt.Sprintf("%d:%d", p.uid(), p.gid())
	if p.rootless() {
		user = "0:0"
	}
	// Un UID sin entrada en /etc/passwd tendría HOME=/, donde las herramientas no pueden escribir.
	return []string{"--user", user, "-e", "HOME=" + containerHome}
}

func currentPlatform() platform {
	return platform{
		goos: runtime.GOOS, uid: os.Getuid, gid: os.Getgid,
		rootless: func() bool {
			// Si no se puede preguntar se asume que no: el fallo de `docker run` posterior ya lo explica.
			out, err := exec.Command("docker", "info", "--format", "{{.SecurityOptions}}").Output()
			return err == nil && strings.Contains(string(out), "rootless")
		},
	}
}
