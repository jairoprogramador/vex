package sigstore

import (
	"context"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"

	"github.com/sigstore/sigstore-go/pkg/verify"

	"github.com/jairoprogramador/vex/internal/application"
)

// Registry es lo que la verificación necesita del registro de imágenes.
type Registry interface {
	// ResolveDigest dice a qué digest apunta hoy repo:tag.
	ResolveDigest(ctx context.Context, repository, tag string) (string, error)
	// Bundles trae las firmas (bundles de Sigstore) que el registro guarda junto a repo@digest.
	Bundles(ctx context.Context, repository, digest string) ([][]byte, error)
}

var (
	digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	runtimeName   = regexp.MustCompile(`^vex-runtime-[a-z0-9-]+$`)
)

// ImageVerifier comprueba que una imagen de runtime la firmó el workflow oficial que las publica.
//
// cosign v3 (el que usan esos workflows) guarda la firma de una imagen como un bundle de Sigstore asociado al
// digest (referrer OCI de tipo BundleArtifactType). El bundle lleva una atestación (sobre DSSE) cuyo sujeto es
// el digest de la imagen: por eso se verifica con el digest y no con un payload reconstruido. La atestación del
// SBOM que publica el mismo workflow tiene la misma forma y la misma identidad; cualquiera de las dos prueba lo
// que importa aquí: que ese workflow, desde un tag de versión, respondió por ese digest.
type ImageVerifier struct {
	engine   *engine
	registry Registry
}

var _ application.ImageVerifier = (*ImageVerifier)(nil)

// NewImageVerifier guarda la raíz de confianza de Sigstore en cacheDir y consulta el registro con registry.
// requirePins es true en un release: ver pins.go.
func NewImageVerifier(cacheDir string, registry Registry, requirePins bool) *ImageVerifier {
	return &ImageVerifier{engine: newEngine(cacheDir, requirePins), registry: registry}
}

func (v *ImageVerifier) Verify(ctx context.Context, ref application.ImageRef) (application.TrustedImage, error) {
	none := application.TrustedImage{}
	fail := func(reason error, signer string, cause error) (application.TrustedImage, error) {
		failure := &application.ImageVerificationError{Image: ref.Repository, Tag: ref.Tag, Reason: reason, Signer: signer}
		if cause != nil {
			failure.Detail = cause.Error()
		}
		return none, failure
	}

	digest, mutable, err := v.pin(ctx, ref)
	if err != nil {
		reason := application.ErrImageRegistry
		if errors.Is(err, application.ErrImageDigestMoved) {
			reason = application.ErrImageDigestMoved
		}
		return fail(reason, "", err)
	}
	raw, err := hex.DecodeString(strings.TrimPrefix(digest, "sha256:"))
	if err != nil {
		return fail(application.ErrImageRegistry, "", err)
	}
	bundles, err := v.registry.Bundles(ctx, ref.Repository, digest)
	if err != nil {
		return fail(application.ErrImageRegistry, "", err)
	}

	// Basta una firma de la identidad exigida. Si ninguna lo es, se dice quién firmó la que sí era válida.
	var otherSigner string
	var lastErr error
	for _, bundle := range bundles {
		entity, err := v.engine.parse(bundle)
		if err != nil {
			lastErr = err
			continue
		}
		result, err := v.verifyEntity(entity, raw, ref.Repository)
		switch {
		case err == nil:
			return application.TrustedImage{
				Reference: ref.Repository + "@" + digest, Digest: digest, Signer: result.signer, Mutable: mutable,
			}, nil
		case errors.Is(err, errOtherIdentity):
			otherSigner = result.signer
		default:
			lastErr = err
		}
	}
	if otherSigner != "" {
		return fail(application.ErrImageIdentity, otherSigner, nil)
	}
	return fail(application.ErrImageNotSigned, "", lastErr)
}

// verifyEntity exige además que la imagen la firme SU repositorio: jairoprogramador/vex-runtime-x la publica el
// repositorio vex-runtime-x. Si no, quien controle un repositorio de runtime podría firmar la imagen de otro.
func (v *ImageVerifier) verifyEntity(entity verify.SignedEntity, digest []byte, repository string) (verified, error) {
	result, err := v.engine.verify(entity, verify.WithArtifactDigest("sha256", digest), RuntimeIdentityRegexp)
	if err != nil {
		return result, err
	}
	name := repository[strings.LastIndex(repository, "/")+1:]
	if !runtimeName.MatchString(name) || signerRepository(result.signer) != name {
		return result, errOtherIdentity
	}
	return result, nil
}

// pin decide qué digest se verifica y se ejecuta. Con digest fijado es ese; y si además hay tag, el tag debe
// seguir apuntando a él: un tag de versión que cambia de contenido es señal de ataque, no de actualización. Con
// solo tag se resuelve ahora, y lo que se ejecuta es el digest verificado, no el tag.
func (v *ImageVerifier) pin(ctx context.Context, ref application.ImageRef) (digest string, mutable bool, err error) {
	if ref.Digest != "" && !digestPattern.MatchString(ref.Digest) {
		return "", false, errors.New("el digest fijado no es un sha256")
	}
	if ref.Tag == "" {
		if ref.Digest == "" {
			return "", false, errors.New("la imagen no dice ni tag ni digest")
		}
		return ref.Digest, false, nil
	}
	current, err := v.registry.ResolveDigest(ctx, ref.Repository, ref.Tag)
	if err != nil {
		return "", false, err
	}
	if !digestPattern.MatchString(current) {
		return "", false, errors.New("el registro devolvió un digest que no es un sha256")
	}
	if ref.Digest == "" {
		return current, true, nil
	}
	if current != ref.Digest {
		return "", false, application.ErrImageDigestMoved
	}
	return ref.Digest, false, nil
}
