package application

import (
	"errors"
	"fmt"
)

// TemplateCatalog dice qué sabe el catálogo firmado de vex sobre una plantilla de pipeline.
type TemplateCatalog interface {
	// Lookup busca la plantilla de pipelineURL en commit. Devuelve error cuando la plantilla debería estar
	// respaldada por el catálogo y no se pudo comprobar: quien llama se detiene.
	Lookup(pipelineURL, commit string) (CatalogListing, error)
}

// CatalogListing es lo que el catálogo firmado dice de una plantilla en un commit.
type CatalogListing struct {
	// Listed: el catálogo firmado publica exactamente esa plantilla en ese commit.
	Listed bool
	// BootstrapSHA256 es la huella de su bootstrap según el catálogo.
	BootstrapSHA256 string
}

// TemplateOrigin dice quién responde de la plantilla que se va a usar.
type TemplateOrigin int

const (
	// TemplateUnsigned: nadie más la revisó; responde quien la usa.
	TemplateUnsigned TemplateOrigin = iota
	// TemplateSigned: es la que publica el catálogo firmado de vex, byte a byte.
	TemplateSigned
)

// UnsignedTemplateNotice es el aviso de una plantilla que el catálogo firmado no respalda.
const UnsignedTemplateNotice = "Plantilla sin firma de vex: la revisas tú. " +
	"El catálogo firmado de vex no respalda este pipeline en este commit; lo que su bootstrap concede es responsabilidad de quien lo acepta."

// ErrTemplateMismatch: el bootstrap del pipeline no es el que el catálogo firmado publica para ese commit.
var ErrTemplateMismatch = errors.New("el bootstrap del pipeline no coincide con el catálogo firmado")

// TemplateMismatchError es ErrTemplateMismatch con de qué plantilla se trata.
type TemplateMismatchError struct {
	PipelineURL string
	Commit      string
}

func (e *TemplateMismatchError) Error() string {
	return fmt.Sprintf("%s@%s: %v", e.PipelineURL, e.Commit, ErrTemplateMismatch)
}

func (e *TemplateMismatchError) Is(target error) bool { return target == ErrTemplateMismatch }

// CheckTemplateOrigin compara el bootstrap que se va a usar con el del catálogo firmado. Una plantilla del
// catálogo tiene que coincidir con su huella; una que no está en él se permite, y quien llama lo avisa.
// Sin catálogo (catalog nil) ninguna plantilla está respaldada.
func CheckTemplateOrigin(catalog TemplateCatalog, pipelineURL, commit string, bootstrap PipelineBootstrap) (TemplateOrigin, error) {
	if catalog == nil {
		return TemplateUnsigned, nil
	}
	listing, err := catalog.Lookup(pipelineURL, commit)
	switch {
	case err != nil:
		return TemplateUnsigned, err
	case !listing.Listed:
		return TemplateUnsigned, nil
	case listing.BootstrapSHA256 == "" || listing.BootstrapSHA256 != bootstrap.Fingerprint:
		return TemplateUnsigned, &TemplateMismatchError{PipelineURL: pipelineURL, Commit: commit}
	}
	return TemplateSigned, nil
}
