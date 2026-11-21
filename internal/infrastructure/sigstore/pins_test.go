package sigstore

import (
	"context"
	"errors"
	"testing"

	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"

	"github.com/jairoprogramador/vex/internal/application"
)

// B8 de la revisión de seguridad: además del nombre, el certificado lleva los ids de la cuenta y del repositorio.

func summary(signer, owner, repo string) certificate.Summary {
	return certificate.Summary{SubjectAlternativeName: signer, Extensions: certificate.Extensions{
		SourceRepositoryOwnerIdentifier: owner, SourceRepositoryIdentifier: repo,
	}}
}

func TestPins_UnReleaseSinIdsNoVerificaNada(t *testing.T) {
	for name, tt := range map[string]struct{ owner, catalog, runtimes string }{
		"nada":                {},
		"sin id del catálogo": {owner: "100"},
		"sin id de la cuenta": {catalog: "200"},
		"solo los de runtime": {runtimes: "vex-runtime-node=300"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parsePins(true, tt.owner, tt.catalog, tt.runtimes); !errors.Is(err, errPinsMissing) {
				t.Fatalf("error = %v; se esperaba errPinsMissing", err)
			}
		})
	}
	t.Run("el motor falla antes de pedir la raíz de confianza", func(t *testing.T) {
		l := newLab(t)
		engine := l.engine()
		engine.pins = func() (identityPins, error) { return loadPins(true) }
		registry := &fakeRegistry{bundles: map[string][][]byte{digestA: {l.attest(officialRuntime, OIDCIssuer, digestA)}}}
		verifier := &ImageVerifier{engine: engine, registry: registry}

		_, err := verifier.Verify(context.Background(), application.ImageRef{Repository: repository, Digest: digestA})

		failure := imageError(t, err, application.ErrImageNotSigned)
		if failure.Detail != errPinsMissing.Error() {
			t.Errorf("detalle = %q; debe decir por qué no se verificó", failure.Detail)
		}
	})
	t.Run("un build de desarrollo sin ids verifica por nombre", func(t *testing.T) {
		pins, err := parsePins(false, "", "", "")
		if err != nil || !pins.check(summary(officialRuntime, "", "")) {
			t.Fatalf("pins = %+v, error = %v", pins, err)
		}
	})
	t.Run("ids a medias en desarrollo también es un error", func(t *testing.T) {
		if _, err := parsePins(false, "100", "", ""); !errors.Is(err, errPinsMissing) {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestPins_IdsMalEscritos(t *testing.T) {
	for name, tt := range map[string]struct{ owner, catalog, runtimes string }{
		"cuenta no numérica":         {"jairo", "200", ""},
		"catálogo no numérico":       {"100", "abc", ""},
		"runtime sin id":             {"100", "200", "vex-runtime-node"},
		"runtime con id no numérico": {"100", "200", "vex-runtime-node=x"},
		"runtime con otro nombre":    {"100", "200", "otro-repo=300"},
		"lista con hueco":            {"100", "200", "vex-runtime-node=300,"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parsePins(true, tt.owner, tt.catalog, tt.runtimes); err == nil {
				t.Fatal("se esperaba un error")
			}
		})
	}
}

func TestPins_ElCertificadoDebeLlevarLosIdsFijados(t *testing.T) {
	pins, err := parsePins(true, "100", "200", "vex-runtime-node=300")
	if err != nil {
		t.Fatal(err)
	}
	const otherRuntime = "https://github.com/jairoprogramador/vex-runtime-java/.github/workflows/docker-publish.yml@refs/tags/v1.0.0"
	for name, tt := range map[string]struct {
		cert certificate.Summary
		want bool
	}{
		"catálogo con sus ids":                       {summary(officialCatalog, "100", "200"), true},
		"runtime con sus ids":                        {summary(officialRuntime, "100", "300"), true},
		"runtime sin id fijado: basta la cuenta":     {summary(otherRuntime, "100", "999"), true},
		"mismo nombre, otra cuenta (nombre reusado)": {summary(officialCatalog, "666", "200"), false},
		"mismo nombre, otro repositorio (recreado)":  {summary(officialCatalog, "100", "201"), false},
		"runtime recreado":                           {summary(officialRuntime, "100", "301"), false},
		"certificado sin ids":                        {summary(officialCatalog, "", ""), false},
		"runtime sin id fijado de otra cuenta":       {summary(otherRuntime, "666", "999"), false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := pins.check(tt.cert); got != tt.want {
				t.Errorf("check = %v", got)
			}
		})
	}
}

// Cada imagen la firma su repositorio: el de otro runtime, aunque sea oficial, no vale.
func TestImagenLigadaASuRepositorio(t *testing.T) {
	const otherRuntime = "https://github.com/jairoprogramador/vex-runtime-java/.github/workflows/docker-publish.yml@refs/tags/v1.0.0"
	for name, tt := range map[string]struct {
		image, signer string
		reason        error
	}{
		"su repositorio":                {repository, officialRuntime, nil},
		"sin registro en el nombre":     {"jairoprogramador/vex-runtime-node", officialRuntime, nil},
		"firmada por otro runtime":      {repository, otherRuntime, application.ErrImageIdentity},
		"imagen que no es un runtime":   {"docker.io/jairoprogramador/node", officialRuntime, application.ErrImageIdentity},
		"nombre que solo empieza igual": {"docker.io/atacante/vex-runtime-node-evil", officialRuntime, application.ErrImageIdentity},
	} {
		t.Run(name, func(t *testing.T) {
			l := newLab(t)
			registry := &fakeRegistry{bundles: map[string][][]byte{digestA: {l.attest(tt.signer, OIDCIssuer, digestA)}}}
			verifier := &ImageVerifier{engine: l.engine(), registry: registry}

			_, err := verifier.Verify(context.Background(), application.ImageRef{Repository: tt.image, Digest: digestA})

			if tt.reason == nil {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if failure := imageError(t, err, tt.reason); failure.Signer != tt.signer {
				t.Errorf("firmante = %q; se dice quién firmó", failure.Signer)
			}
		})
	}
}

// Con ids fijados, una firma válida de nombre correcto cuyo certificado no los lleva es de «otra identidad».
func TestMotor_NombreCorrectoConOtrosIdsEsOtraIdentidad(t *testing.T) {
	l := newLab(t)
	engine := l.engine()
	// La autoridad de mentira no escribe ids: su certificado nunca lleva los fijados.
	engine.pins = func() (identityPins, error) { return parsePins(true, "100", "200", "") }
	registry := &fakeRegistry{bundles: map[string][][]byte{digestA: {l.attest(officialRuntime, OIDCIssuer, digestA)}}}

	_, err := (&ImageVerifier{engine: engine, registry: registry}).Verify(context.Background(),
		application.ImageRef{Repository: repository, Digest: digestA})

	if failure := imageError(t, err, application.ErrImageIdentity); failure.Signer != officialRuntime {
		t.Errorf("firmante = %q", failure.Signer)
	}
}
