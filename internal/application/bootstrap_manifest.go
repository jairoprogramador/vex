package application

import (
	"errors"
	"fmt"
)

// BootstrapManifest es bootstrap/<nube>/bootstrap.yaml: qué contrato de nube exige el pipeline, qué plantilla lo
// prepara y qué pasos reciben el token. La CLI no interpreta Template ni Providers al ejecutar un paso: los usa
// `vex bootstrap`.
type BootstrapManifest struct {
	Contract ContractID
	// Template es el archivo de la carpeta del bootstrap que se aplica en la nube.
	Template string
	// Providers son los espacios de nombres que la nube debe tener registrados.
	Providers []string
	// CloudSteps son los pasos que reciben el token; nunca viene vacío.
	CloudSteps []string
}

// PipelineBootstrap es el bootstrap que trae un pipeline para una nube.
type PipelineBootstrap struct {
	// Cloud es el nombre de la carpeta ("azure") y Dir, su ruta local.
	Cloud string
	Dir   string
	// Fingerprint es la huella (sha256 en hexadecimal) de todo el contenido de la carpeta: cambia con cualquier
	// modificación del manifiesto o de la plantilla.
	Fingerprint string
	Manifest    BootstrapManifest
	// Template son los bytes de la plantilla tal como se leyeron al calcular la huella. El adaptador valida,
	// previsualiza y aplica estos bytes, nunca una segunda lectura del disco.
	Template []byte
}

// BootstrapReader lee el bootstrap de un pipeline clonado.
type BootstrapReader interface {
	// Read lee bootstrap/<provider>/; con provider "" exige que el pipeline traiga exactamente una nube.
	Read(pipelineDir, provider string) (PipelineBootstrap, error)
}

// ErrBootstrapOutdated: la conexión se creó con otro bootstrap que el del pipeline que se va a ejecutar.
var ErrBootstrapOutdated = errors.New("el bootstrap del pipeline cambió")

// BootstrapOutdatedError es ErrBootstrapOutdated con el ambiente y el motivo. El mensaje dice qué ejecutar.
type BootstrapOutdatedError struct {
	Environment string
	Detail      string
}

func (e *BootstrapOutdatedError) Error() string {
	return fmt.Sprintf("El bootstrap del pipeline cambió: ejecuta `vex bootstrap %s` (%s)", e.Environment, e.Detail)
}

func (e *BootstrapOutdatedError) Is(target error) bool { return target == ErrBootstrapOutdated }

// CheckConnection comprueba, sin hablar con la nube, que la conexión de environment se creó con el bootstrap de
// este pipeline: mismo contrato y misma huella. Si no, devuelve un *BootstrapOutdatedError: quien aceptó la
// conexión aceptó otra cosa, y solo `vex bootstrap` puede volver a aceptarla.
func CheckConnection(bootstrap PipelineBootstrap, conn CloudConnectionInfo, environment string) error {
	outdated := func(format string, args ...any) error {
		return &BootstrapOutdatedError{Environment: environment, Detail: fmt.Sprintf(format, args...)}
	}
	switch {
	case conn.Outputs == nil:
		return outdated("la conexión no describe lo que hay en la nube")
	case conn.Outputs.ContractID != bootstrap.Manifest.Contract:
		return outdated("la conexión es %q y el pipeline pide %q", conn.Outputs.ContractID, bootstrap.Manifest.Contract)
	case conn.BootstrapSHA256 == "" || conn.BootstrapSHA256 != bootstrap.Fingerprint:
		return outdated("la conexión se aceptó con otra versión de bootstrap/%s/", bootstrap.Cloud)
	}
	return nil
}
