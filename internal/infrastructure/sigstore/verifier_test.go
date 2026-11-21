package sigstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/testing/ca"
	"github.com/sigstore/sigstore-go/pkg/verify"

	"github.com/jairoprogramador/vex/internal/application"
	"github.com/jairoprogramador/vex/internal/domain/architecture/ports"
)

const (
	officialRuntime = "https://github.com/jairoprogramador/vex-runtime-node/.github/workflows/docker-publish.yml@refs/tags/v1.2.3"
	officialCatalog = "https://github.com/jairoprogramador/vex-template-store/.github/workflows/publish-templates.yml@refs/tags/v2026.11.21-1"
	repository      = "docker.io/jairoprogramador/vex-runtime-node"
)

var (
	digestA = "sha256:" + strings.Repeat("a", 64)
	digestB = "sha256:" + strings.Repeat("b", 64)
)

// lab es un Sigstore de mentira: firma de verdad, con su propia raíz, sin red. Las firmas se guardan por nombre
// y el motor las «lee» por ese nombre, en lugar de serializar bundles.
type lab struct {
	t        *testing.T
	sigstore *ca.VirtualSigstore
	entities map[string]verify.SignedEntity
}

func newLab(t *testing.T) *lab {
	t.Helper()
	virtual, err := ca.NewVirtualSigstore()
	if err != nil {
		t.Fatal(err)
	}
	return &lab{t: t, sigstore: virtual, entities: map[string]verify.SignedEntity{}}
}

func (l *lab) engine() *engine {
	return &engine{
		trusted: func() (root.TrustedMaterial, error) { return l.sigstore, nil },
		// La autoridad de mentira no escribe ids en el certificado: aquí se verifica por nombre (pins_test.go).
		pins: func() (identityPins, error) { return identityPins{}, nil },
		// La autoridad de mentira no emite SCT; lo demás se exige igual que en producción.
		options: []verify.VerifierOption{verify.WithTransparencyLog(1), verify.WithObserverTimestamps(1)},
		parse: func(raw []byte) (verify.SignedEntity, error) {
			entity, ok := l.entities[string(raw)]
			if !ok {
				return nil, errors.New("bundle ilegible")
			}
			return entity, nil
		},
	}
}

// attest firma, como identity, una atestación cuyo sujeto es digest, y devuelve el nombre de la firma.
func (l *lab) attest(identity, issuer, digest string) []byte {
	l.t.Helper()
	statement := fmt.Sprintf(`{"_type":"https://in-toto.io/Statement/v1","subject":[{"name":"imagen","digest":{"sha256":%q}}],"predicateType":"https://sigstore.dev/cosign/sign/v1","predicate":{}}`,
		strings.TrimPrefix(digest, "sha256:"))
	entity, err := l.sigstore.Attest(identity, issuer, []byte(statement))
	if err != nil {
		l.t.Fatal(err)
	}
	return l.keep(entity)
}

func (l *lab) sign(identity, issuer string, artifact []byte) []byte {
	l.t.Helper()
	entity, err := l.sigstore.Sign(identity, issuer, artifact)
	if err != nil {
		l.t.Fatal(err)
	}
	return l.keep(entity)
}

func (l *lab) keep(entity verify.SignedEntity) []byte {
	name := fmt.Sprintf("firma-%d", len(l.entities))
	l.entities[name] = entity
	return []byte(name)
}

type fakeRegistry struct {
	tags       map[string]string   // repo:tag → digest
	bundles    map[string][][]byte // digest → firmas
	resolveErr error
	resolved   int
}

func (r *fakeRegistry) ResolveDigest(_ context.Context, repo, tag string) (string, error) {
	r.resolved++
	if r.resolveErr != nil {
		return "", r.resolveErr
	}
	digest, ok := r.tags[repo+":"+tag]
	if !ok {
		return "", errors.New("MANIFEST_UNKNOWN")
	}
	return digest, nil
}

func (r *fakeRegistry) Bundles(_ context.Context, _, digest string) ([][]byte, error) {
	return r.bundles[digest], nil
}

func imageError(t *testing.T, err error, reason error) *application.ImageVerificationError {
	t.Helper()
	var failure *application.ImageVerificationError
	if !errors.As(err, &failure) || !errors.Is(err, reason) {
		t.Fatalf("error = %v; se esperaba %v", err, reason)
	}
	return failure
}

func TestSEC31_ImagenFirmadaOficial(t *testing.T) {
	l := newLab(t)
	registry := &fakeRegistry{
		tags:    map[string]string{repository + ":1.2.3": digestA},
		bundles: map[string][][]byte{digestA: {l.attest(officialRuntime, OIDCIssuer, digestA)}},
	}
	verifier := &ImageVerifier{engine: l.engine(), registry: registry}

	got, err := verifier.Verify(context.Background(), application.ImageRef{Repository: repository, Tag: "1.2.3", Digest: digestA})
	if err != nil {
		t.Fatal(err)
	}
	if got.Reference != repository+"@"+digestA || got.Digest != digestA || got.Mutable || got.Signer != officialRuntime {
		t.Errorf("imagen de confianza = %+v", got)
	}
}

func TestSEC31_SoloDigestNoConsultaElTag(t *testing.T) {
	l := newLab(t)
	registry := &fakeRegistry{bundles: map[string][][]byte{digestA: {l.attest(officialRuntime, OIDCIssuer, digestA)}}}
	verifier := &ImageVerifier{engine: l.engine(), registry: registry}

	got, err := verifier.Verify(context.Background(), application.ImageRef{Repository: repository, Digest: digestA})
	if err != nil {
		t.Fatal(err)
	}
	if got.Reference != repository+"@"+digestA || got.Mutable || registry.resolved != 0 {
		t.Errorf("imagen = %+v, resoluciones de tag = %d", got, registry.resolved)
	}
}

func TestSEC31_ImagenSinFirma(t *testing.T) {
	l := newLab(t)
	registry := &fakeRegistry{tags: map[string]string{repository + ":1.2.3": digestA}}
	verifier := &ImageVerifier{engine: l.engine(), registry: registry}

	_, err := verifier.Verify(context.Background(), application.ImageRef{Repository: repository, Tag: "1.2.3"})
	imageError(t, err, application.ErrImageNotSigned)
}

func TestSEC31_FirmaDeOtroDigestNoVale(t *testing.T) {
	l := newLab(t)
	// Una firma oficial auténtica, pero de otra imagen, colgada de esta.
	registry := &fakeRegistry{bundles: map[string][][]byte{digestA: {l.attest(officialRuntime, OIDCIssuer, digestB)}}}
	verifier := &ImageVerifier{engine: l.engine(), registry: registry}

	_, err := verifier.Verify(context.Background(), application.ImageRef{Repository: repository, Digest: digestA})
	imageError(t, err, application.ErrImageNotSigned)
}

func TestSEC31_FirmaDeOtraRaizNoVale(t *testing.T) {
	l, other := newLab(t), newLab(t)
	foreign, err := other.sigstore.Attest(officialRuntime, OIDCIssuer, []byte(fmt.Sprintf(
		`{"_type":"https://in-toto.io/Statement/v1","subject":[{"name":"imagen","digest":{"sha256":%q}}],"predicateType":"x","predicate":{}}`,
		strings.Repeat("a", 64))))
	if err != nil {
		t.Fatal(err)
	}
	registry := &fakeRegistry{bundles: map[string][][]byte{digestA: {l.keep(foreign)}}}
	verifier := &ImageVerifier{engine: l.engine(), registry: registry}

	_, err = verifier.Verify(context.Background(), application.ImageRef{Repository: repository, Digest: digestA})
	imageError(t, err, application.ErrImageNotSigned)
}

func TestSEC31_OtraIdentidad(t *testing.T) {
	tests := []struct{ name, identity, issuer string }{
		{"otro repo", "https://github.com/jairoprogramador/otro-repo/.github/workflows/docker-publish.yml@refs/tags/v1.0.0", OIDCIssuer},
		{"otro workflow", "https://github.com/jairoprogramador/vex-runtime-x/.github/workflows/otro.yml@refs/tags/v1.0.0", OIDCIssuer},
		{"otra organización", "https://github.com/atacante/vex-runtime-node/.github/workflows/docker-publish.yml@refs/tags/v1.0.0", OIDCIssuer},
		{"desde una rama", "https://github.com/jairoprogramador/vex-runtime-node/.github/workflows/docker-publish.yml@refs/heads/main", OIDCIssuer},
		{"sufijo añadido", officialRuntime + "-rc/../x", OIDCIssuer},
		{"otro emisor", officialRuntime, "https://accounts.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newLab(t)
			registry := &fakeRegistry{bundles: map[string][][]byte{digestA: {l.attest(tt.identity, tt.issuer, digestA)}}}
			verifier := &ImageVerifier{engine: l.engine(), registry: registry}

			_, err := verifier.Verify(context.Background(), application.ImageRef{Repository: repository, Digest: digestA})
			failure := imageError(t, err, application.ErrImageIdentity)
			if failure.Signer != tt.identity {
				t.Errorf("firmante = %q", failure.Signer)
			}
		})
	}
}

func TestSEC31_BastaUnaFirmaOficialEntreVarias(t *testing.T) {
	l := newLab(t)
	registry := &fakeRegistry{bundles: map[string][][]byte{digestA: {
		[]byte("basura"),
		l.attest("https://github.com/atacante/x/.github/workflows/docker-publish.yml@refs/tags/v1.0.0", OIDCIssuer, digestA),
		l.attest(officialRuntime, OIDCIssuer, digestA),
	}}}
	verifier := &ImageVerifier{engine: l.engine(), registry: registry}

	if _, err := verifier.Verify(context.Background(), application.ImageRef{Repository: repository, Digest: digestA}); err != nil {
		t.Fatal(err)
	}
}

func TestSEC31_DigestDistinto(t *testing.T) {
	l := newLab(t)
	registry := &fakeRegistry{
		tags: map[string]string{repository + ":1.2.3": digestB},
		bundles: map[string][][]byte{
			digestA: {l.attest(officialRuntime, OIDCIssuer, digestA)},
			digestB: {l.attest(officialRuntime, OIDCIssuer, digestB)},
		},
	}
	verifier := &ImageVerifier{engine: l.engine(), registry: registry}

	_, err := verifier.Verify(context.Background(), application.ImageRef{Repository: repository, Tag: "1.2.3", Digest: digestA})
	imageError(t, err, application.ErrImageDigestMoved)
}

func TestSEC31_TagMutableSeEjecutaPorDigest(t *testing.T) {
	l := newLab(t)
	registry := &fakeRegistry{
		tags:    map[string]string{repository + ":1.2.3": digestA},
		bundles: map[string][][]byte{digestA: {l.attest(officialRuntime, OIDCIssuer, digestA)}},
	}
	verifier := &ImageVerifier{engine: l.engine(), registry: registry}

	got, err := verifier.Verify(context.Background(), application.ImageRef{Repository: repository, Tag: "1.2.3"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Reference != repository+"@"+digestA || !got.Mutable {
		t.Errorf("imagen = %+v", got)
	}
}

func TestSEC31_RegistroInalcanzableFallaCerrado(t *testing.T) {
	l := newLab(t)
	verifier := &ImageVerifier{engine: l.engine(), registry: &fakeRegistry{resolveErr: errors.New("dial tcp: timeout")}}

	_, err := verifier.Verify(context.Background(), application.ImageRef{Repository: repository, Tag: "1.2.3"})
	imageError(t, err, application.ErrImageRegistry)
}

func TestSEC31_SinRaizDeConfianzaFallaCerrado(t *testing.T) {
	l := newLab(t)
	engine := l.engine()
	engine.trusted = func() (root.TrustedMaterial, error) { return nil, errors.New("sin red") }
	registry := &fakeRegistry{bundles: map[string][][]byte{digestA: {l.attest(officialRuntime, OIDCIssuer, digestA)}}}
	verifier := &ImageVerifier{engine: engine, registry: registry}

	_, err := verifier.Verify(context.Background(), application.ImageRef{Repository: repository, Digest: digestA})
	imageError(t, err, application.ErrImageNotSigned)
}

func TestCatalogVerifier(t *testing.T) {
	catalog := []byte(`[{"stack":"node"}]`)
	tests := []struct {
		name, identity, issuer string
		signed, presented      []byte
		wantErr                bool
	}{
		{"firma oficial", officialCatalog, OIDCIssuer, catalog, catalog, false},
		{"contenido alterado", officialCatalog, OIDCIssuer, catalog, []byte(`[{"stack":"otro"}]`), true},
		{"firmado desde una rama", "https://github.com/jairoprogramador/vex-template-store/.github/workflows/publish-templates.yml@refs/heads/main", OIDCIssuer, catalog, catalog, true},
		{"firmado por un fork", "https://github.com/atacante/vex-template-store/.github/workflows/publish-templates.yml@refs/tags/v2026.11.21-1", OIDCIssuer, catalog, catalog, true},
		{"firma de imagen de runtime no vale para el catálogo", officialRuntime, OIDCIssuer, catalog, catalog, true},
		{"otro emisor", officialCatalog, "https://accounts.example.com", catalog, catalog, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newLab(t)
			verifier := &CatalogVerifier{engine: l.engine()}

			signedAt, err := verifier.Verify(tt.presented, l.sign(tt.identity, tt.issuer, tt.signed))
			if tt.wantErr {
				if !errors.Is(err, ports.ErrCatalogSignature) {
					t.Fatalf("error = %v; se esperaba ErrCatalogSignature", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if time.Since(signedAt).Abs() > time.Hour {
				t.Errorf("hora de firma = %v", signedAt)
			}
		})
	}
}

func TestCatalogVerifierRechazaBundleIlegible(t *testing.T) {
	verifier := NewCatalogVerifier(t.TempDir(), false)
	if _, err := verifier.Verify([]byte("x"), []byte("{no es un bundle")); !errors.Is(err, ports.ErrCatalogSignature) {
		t.Fatalf("error = %v", err)
	}
}

func TestIdentidadesAncladas(t *testing.T) {
	for _, pattern := range []string{RuntimeIdentityRegexp, CatalogIdentityRegexp} {
		if !strings.HasPrefix(pattern, "^") || !strings.HasSuffix(pattern, "$") {
			t.Errorf("la identidad %q no está anclada: aceptaría prefijos o sufijos", pattern)
		}
	}
}
