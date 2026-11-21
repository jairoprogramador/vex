package application

import (
	"context"
	"errors"
)

// ImageVerifier comprueba que una imagen de runtime la publicó quien dice vex antes de darle credenciales.
type ImageVerifier interface {
	// Verify resuelve la referencia a un digest, verifica su firma y devuelve la referencia inmutable que se
	// puede ejecutar. Los errores son *ImageVerificationError.
	Verify(ctx context.Context, ref ImageRef) (TrustedImage, error)
}

// ImageRef es la imagen tal como la escribe vexconfig.yaml.
type ImageRef struct {
	Repository string
	Tag        string // puede venir vacío
	Digest     string // "sha256:…"; puede venir vacío
}

// TrustedImage es una imagen verificada.
type TrustedImage struct {
	// Reference es repo@sha256:…: lo que se ejecuta.
	Reference string
	Digest    string
	// Signer es la identidad que la firmó, para mostrarla.
	Signer string
	// Mutable: el digest salió de resolver un tag, que mañana puede apuntar a otra cosa.
	Mutable bool
}

var (
	ErrImageNotSigned   = errors.New("la imagen no tiene una firma válida")
	ErrImageIdentity    = errors.New("la imagen está firmada por otra identidad")
	ErrImageDigestMoved = errors.New("el tag apunta a otro digest que el fijado")
	ErrImageRegistry    = errors.New("no se pudo consultar el registro de la imagen")
)

// ImageVerificationError dice qué imagen no pasó la verificación y por qué (Reason es uno de los ErrImage…).
type ImageVerificationError struct {
	Image  string
	Tag    string
	Reason error
	// Signer es quien la firmó, cuando la firma es válida pero de otra identidad.
	Signer string
	// Detail es la causa técnica; no se muestra a quien usa la CLI.
	Detail string
}

func (e *ImageVerificationError) Error() string { return e.Image + ": " + e.Reason.Error() }
func (e *ImageVerificationError) Unwrap() error { return e.Reason }
