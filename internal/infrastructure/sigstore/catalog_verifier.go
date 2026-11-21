package sigstore

import (
	"bytes"
	"fmt"
	"time"

	"github.com/sigstore/sigstore-go/pkg/verify"

	"github.com/jairoprogramador/vex/internal/domain/architecture/ports"
)

// CatalogVerifier comprueba que el catálogo de plantillas lo firmó el workflow que lo publica.
type CatalogVerifier struct{ engine *engine }

var _ ports.CatalogVerifier = (*CatalogVerifier)(nil)

// NewCatalogVerifier guarda la raíz de confianza de Sigstore en cacheDir. requirePins es true en un release: ver
// pins.go.
func NewCatalogVerifier(cacheDir string, requirePins bool) *CatalogVerifier {
	return &CatalogVerifier{engine: newEngine(cacheDir, requirePins)}
}

// Verify devuelve cuándo se firmó data. La hora la atestigua el registro de transparencia: sirve para rechazar
// un catálogo antiguo, también firmado, que alguien quiera hacer pasar por el vigente.
func (v *CatalogVerifier) Verify(data, bundle []byte) (time.Time, error) {
	entity, err := v.engine.parse(bundle)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %v", ports.ErrCatalogSignature, err)
	}
	return v.verifyEntity(data, entity)
}

func (v *CatalogVerifier) verifyEntity(data []byte, entity verify.SignedEntity) (time.Time, error) {
	result, err := v.engine.verify(entity, verify.WithArtifact(bytes.NewReader(data)), CatalogIdentityRegexp)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %v", ports.ErrCatalogSignature, err)
	}
	return result.signedAt, nil
}
