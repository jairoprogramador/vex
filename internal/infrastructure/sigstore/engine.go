package sigstore

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

// errOtherIdentity: la firma es criptográficamente válida, pero no es de la identidad exigida.
var errOtherIdentity = errors.New("firmado por otra identidad")

// verified es lo que se sabe de una firma que pasó la verificación.
type verified struct {
	signer   string
	signedAt time.Time
}

// engine verifica una firma contra la raíz de confianza pública de Sigstore. La raíz se obtiene la primera vez
// que hace falta: los comandos que no verifican nada no pagan esa red.
type engine struct {
	trusted func() (root.TrustedMaterial, error)
	options []verify.VerifierOption
	// parse lee una firma tal como llega (del registro o de la descarga).
	parse func(raw []byte) (verify.SignedEntity, error)
	// pins da los ids de cuenta y repositorio que debe llevar el certificado.
	pins func() (identityPins, error)
}

// newEngine usa la raíz pública de Sigstore, traída por TUF y guardada en cacheDir. Con requirePins (un release),
// si el binario no lleva los ids de quien firma no verifica nada. Exige lo que una firma sin
// llaves pública trae: el certificado en un registro de transparencia de certificados, la firma en el registro
// de transparencia y una hora de firma atestiguada por un tercero.
func newEngine(cacheDir string, requirePins bool) *engine {
	return &engine{
		pins: sync.OnceValues(func() (identityPins, error) { return loadPins(requirePins) }),
		trusted: sync.OnceValues(func() (root.TrustedMaterial, error) {
			// Un día de validez: sin red se sigue verificando con la raíz guardada mientras sus metadatos valgan.
			opts := tuf.DefaultOptions().WithCachePath(cacheDir).WithCacheValidity(1)
			return root.FetchTrustedRootWithOptions(opts)
		}),
		options: []verify.VerifierOption{
			verify.WithSignedCertificateTimestamps(1),
			verify.WithTransparencyLog(1),
			verify.WithObserverTimestamps(1),
		},
		parse: parseBundle,
	}
}

// parseBundle lee un bundle de Sigstore tal como lo escribe cosign.
func parseBundle(raw []byte) (verify.SignedEntity, error) {
	var parsed bundle.Bundle
	if err := parsed.UnmarshalJSON(raw); err != nil {
		return nil, fmt.Errorf("el bundle de la firma no se entiende: %w", err)
	}
	return &parsed, nil
}

// verify comprueba que entity firma el artefacto y que el certificado es de un workflow de GitHub cuya
// identidad cumple identityRegexp. Si la firma vale pero es de otro, devuelve errOtherIdentity con quién.
func (e *engine) verify(entity verify.SignedEntity, artifact verify.ArtifactPolicyOption, identityRegexp string) (verified, error) {
	// Antes que nada, y antes de tocar la red: sin saber a quién exigir, no se verifica.
	pins, err := e.pins()
	if err != nil {
		return verified{}, err
	}
	trusted, err := e.trusted()
	if err != nil {
		return verified{}, fmt.Errorf("no se pudo obtener la raíz de confianza de Sigstore: %w", err)
	}
	verifier, err := verify.NewVerifier(trusted, e.options...)
	if err != nil {
		return verified{}, err
	}
	identity, err := verify.NewShortCertificateIdentity(OIDCIssuer, "", "", identityRegexp)
	if err != nil {
		return verified{}, err
	}

	result, err := verifier.Verify(entity, verify.NewPolicy(artifact, verify.WithCertificateIdentity(identity)))
	var mismatch *verify.ErrNoMatchingCertificateIdentity
	switch {
	case errors.As(err, &mismatch):
		return verified{signer: signerOf(entity)}, errOtherIdentity
	case err != nil:
		return verified{}, err
	case result.Signature == nil || result.Signature.Certificate == nil || len(result.VerifiedTimestamps) == 0:
		return verified{}, errors.New("la verificación no dice quién firmó ni cuándo")
	}
	out := verified{signer: result.Signature.Certificate.SubjectAlternativeName}
	// El nombre coincide; el id dice si sigue siendo la misma cuenta y el mismo repositorio.
	if !pins.check(*result.Signature.Certificate) {
		return out, errOtherIdentity
	}
	for _, ts := range result.VerifiedTimestamps {
		if ts.Timestamp.After(out.signedAt) {
			out.signedAt = ts.Timestamp
		}
	}
	return out, nil
}

// signerOf lee del certificado quién firmó, para poder decirlo cuando no es quien se esperaba.
func signerOf(entity verify.SignedEntity) string {
	content, err := entity.VerificationContent()
	if err != nil || content.Certificate() == nil {
		return ""
	}
	summary, err := certificate.SummarizeCertificate(content.Certificate())
	if err != nil {
		return ""
	}
	return summary.SubjectAlternativeName
}
