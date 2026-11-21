package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	proVos "github.com/jairoprogramador/vex/internal/domain/project/vos"
)

// fakeImageVerifier da por buena cualquier imagen y devuelve la misma referencia que se pidió, salvo que se le
// diga otra cosa: así los arneses que no prueban la verificación ven la imagen de siempre.
type fakeImageVerifier struct {
	trusted *TrustedImage
	err     error
	calls   []ImageRef
}

func (v *fakeImageVerifier) Verify(_ context.Context, ref ImageRef) (TrustedImage, error) {
	v.calls = append(v.calls, ref)
	switch {
	case v.err != nil:
		return TrustedImage{}, v.err
	case v.trusted != nil:
		return *v.trusted, nil
	}
	return TrustedImage{Reference: ref.Repository + ":" + ref.Tag, Digest: ref.Digest}, nil
}

// fakeCatalog responde lo mismo de cualquier plantilla.
type fakeCatalog struct {
	listing CatalogListing
	err     error
	lookups []string
}

func (c *fakeCatalog) Lookup(url, commit string) (CatalogListing, error) {
	c.lookups = append(c.lookups, url+"@"+commit)
	return c.listing, c.err
}

// signedCatalog es un catálogo firmado que publica el bootstrap de las pruebas.
func signedCatalog() *fakeCatalog {
	return &fakeCatalog{listing: CatalogListing{Listed: true, BootstrapSHA256: testFingerprint}}
}

func TestCheckTemplateOrigin(t *testing.T) {
	bootstrap := PipelineBootstrap{Fingerprint: testFingerprint}
	unavailable := errors.New("catálogo sin verificar")
	tests := []struct {
		name    string
		catalog TemplateCatalog
		want    TemplateOrigin
		wantErr error
	}{
		{"sin catálogo nadie la respalda", nil, TemplateUnsigned, nil},
		{"en el catálogo y coincide", signedCatalog(), TemplateSigned, nil},
		{"fuera del catálogo", &fakeCatalog{}, TemplateUnsigned, nil},
		{"en el catálogo con otra huella", &fakeCatalog{listing: CatalogListing{Listed: true, BootstrapSHA256: strings.Repeat("0", 64)}}, TemplateUnsigned, ErrTemplateMismatch},
		{"en el catálogo sin huella", &fakeCatalog{listing: CatalogListing{Listed: true}}, TemplateUnsigned, ErrTemplateMismatch},
		{"el catálogo no se pudo comprobar", &fakeCatalog{err: unavailable}, TemplateUnsigned, unavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CheckTemplateOrigin(tt.catalog, "https://example.com/pipe.git", "abc", bootstrap)
			assert.Equal(t, tt.want, got)
			if tt.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestCloudPreparer_PlantillaFueraDelCatalogoAvisaYSigue(t *testing.T) {
	r := newPreparerRig(t)
	catalog := &fakeCatalog{}
	r.preparer.WithTemplateCatalog(catalog)

	_, err := r.prepare(context.Background())

	require.NoError(t, err)
	assert.Contains(t, r.notify.warnings, UnsignedTemplateNotice)
	assert.Contains(t, UnsignedTemplateNotice, "Plantilla sin firma de vex: la revisas tú")
	assert.Equal(t, []string{"https://github.com/local/pipe.git@c1"}, catalog.lookups, "se compara el commit clonado")
}

func TestCloudPreparer_PlantillaDelCatalogoQueNoCoincideDetiene(t *testing.T) {
	for name, catalog := range map[string]*fakeCatalog{
		"otra huella":            {listing: CatalogListing{Listed: true, BootstrapSHA256: strings.Repeat("0", 64)}},
		"catálogo sin verificar": {err: errors.New("firma inválida")},
	} {
		t.Run(name, func(t *testing.T) {
			r := newPreparerRig(t)
			r.preparer.WithTemplateCatalog(catalog)

			_, err := r.prepare(context.Background())

			require.Error(t, err)
			assert.Empty(t, r.conns.calls, "no se llega a consultar la conexión")
		})
	}
}

func TestSEC31_ImagenSinFirmaNoAbreSesion(t *testing.T) {
	for name, reason := range map[string]error{
		"sin firma":      ErrImageNotSigned,
		"otra identidad": ErrImageIdentity,
		"digest movido":  ErrImageDigestMoved,
		"registro caído": ErrImageRegistry,
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, newProject(t, withCloud(t, "", proVos.CredentialsPortal)))
			credentials := newFakeCredentials()
			WithPortalCredentials(credentials)(h.executor)
			WithImageVerifier(&fakeImageVerifier{err: &ImageVerificationError{Image: "local/runtime", Reason: reason}})(h.executor)

			err := h.executor.Run(context.Background(), "deploy", "sand")

			require.ErrorIs(t, err, reason)
			assert.Empty(t, credentials.requests, "una imagen sin verificar no recibe credenciales")
			assert.Zero(t, h.engine.attempts, "ni se crea ningún contenedor con ella")
		})
	}
}

func TestSEC31_SinVerificadorNoSeEjecutaConPortal(t *testing.T) {
	h := newHarness(t, newProject(t, withCloud(t, "", proVos.CredentialsPortal)))
	credentials := newFakeCredentials()
	WithPortalCredentials(credentials)(h.executor)
	h.executor.verifier = nil

	err := h.executor.Run(context.Background(), "deploy", "sand")

	require.ErrorContains(t, err, "no puede verificar la firma")
	assert.Empty(t, credentials.requests)
}

func TestSEC31_TagMutableSeEjecutaPorDigest(t *testing.T) {
	h := newHarness(t, newProject(t, withCloud(t, "", proVos.CredentialsPortal), withImage("reg.io/vex/node:1.2.3")))
	WithPortalCredentials(newFakeCredentials())(h.executor)
	digest := "sha256:" + strings.Repeat("a", 64)
	verifier := &fakeImageVerifier{trusted: &TrustedImage{Reference: "reg.io/vex/node@" + digest, Digest: digest, Mutable: true}}
	WithImageVerifier(verifier)(h.executor)

	require.NoError(t, h.executor.Run(context.Background(), "deploy", "sand"))

	assert.Equal(t, []ImageRef{{Repository: "reg.io/vex/node", Tag: "1.2.3"}}, verifier.calls)
	assert.Equal(t, "reg.io/vex/node@"+digest, h.engine.spec.Image, "el intento corre el digest verificado")
	require.NotEmpty(t, h.presenter.warnings)
	assert.Contains(t, strings.Join(h.presenter.warnings, "\n"), "Fija el digest")
}

func TestSEC31_ImagenFijadaSeVerificaConSuDigest(t *testing.T) {
	digest := "sha256:" + strings.Repeat("b", 64)
	h := newHarness(t, newProject(t, withCloud(t, "", proVos.CredentialsPortal), withImage("reg.io/vex/node:1.2.3@"+digest)))
	WithPortalCredentials(newFakeCredentials())(h.executor)
	verifier := &fakeImageVerifier{trusted: &TrustedImage{Reference: "reg.io/vex/node@" + digest, Digest: digest}}
	WithImageVerifier(verifier)(h.executor)

	require.NoError(t, h.executor.Run(context.Background(), "deploy", "sand"))

	assert.Equal(t, []ImageRef{{Repository: "reg.io/vex/node", Tag: "1.2.3", Digest: digest}}, verifier.calls)
	assert.NotContains(t, strings.Join(h.presenter.warnings, "\n"), "Fija el digest")
}

func TestSEC31_DockerfileLocalAdvierte(t *testing.T) {
	h := newHarness(t, newProject(t, withCloud(t, "", proVos.CredentialsPortal), withImage("Dockerfile")))
	WithPortalCredentials(newFakeCredentials())(h.executor)
	verifier := &fakeImageVerifier{}
	WithImageVerifier(verifier)(h.executor)

	require.NoError(t, h.executor.Run(context.Background(), "deploy", "sand"))

	assert.Empty(t, verifier.calls, "no hay firma que verificar")
	assert.Contains(t, strings.Join(h.presenter.warnings, "\n"), "vex no puede verificar su firma")
}

func TestSEC31_SinPortalNoSeVerifica(t *testing.T) {
	h := newHarness(t, newProject(t))
	verifier := &fakeImageVerifier{err: errors.New("no debería llamarse")}
	WithImageVerifier(verifier)(h.executor)

	require.NoError(t, h.executor.Run(context.Background(), "deploy", "sand"))
	assert.Empty(t, verifier.calls)
}

func TestSEC31_RollbackTampocoAbreSesionSinFirma(t *testing.T) {
	h := newRollbackHarness(t, withCloud(t, "", proVos.CredentialsPortal))
	credentials := newFakeCredentials()
	WithRollbackPortalCredentials(credentials)(h.service)
	WithRollbackImageVerifier(&fakeImageVerifier{err: &ImageVerificationError{Image: "local/runtime", Reason: ErrImageNotSigned}})(h.service)

	err := h.service.Run(context.Background(), RollbackPlan{DeploymentID: "d-1", Environment: "sand"})

	require.ErrorIs(t, err, ErrImageNotSigned)
	assert.Empty(t, credentials.requests)
}

func TestBootstrap_LaVistaPreviaDiceQuienRespondeDeLaPlantilla(t *testing.T) {
	t.Run("del catálogo firmado", func(t *testing.T) {
		r := newBootstrapRig(t)
		plan, err := r.service.Plan(context.Background(), BootstrapOptions{Environment: "sand", Region: "eastus2"})
		require.NoError(t, err)
		assert.Equal(t, TemplateSigned, plan.Origin)
		assert.Len(t, r.catalog.lookups, 1)
	})
	t.Run("fuera del catálogo se permite, marcada", func(t *testing.T) {
		r := newBootstrapRig(t)
		r.catalog.listing = CatalogListing{}
		plan, err := r.service.Plan(context.Background(), BootstrapOptions{Environment: "sand", Region: "eastus2"})
		require.NoError(t, err)
		assert.Equal(t, TemplateUnsigned, plan.Origin)
	})
}

// Barandilla 4: si la plantilla es del catálogo y su bootstrap no es el publicado, no se mira nada más de ella.
func TestBootstrap_PlantillaDelCatalogoQueNoCoincideNoLlegaALaNube(t *testing.T) {
	for name, catalog := range map[string]fakeCatalog{
		"otra huella":            {listing: CatalogListing{Listed: true, BootstrapSHA256: strings.Repeat("0", 64)}},
		"catálogo sin verificar": {err: errors.New("firma inválida")},
	} {
		t.Run(name, func(t *testing.T) {
			r := newBootstrapRig(t)
			*r.catalog = catalog

			_, err := r.service.Plan(context.Background(), BootstrapOptions{Environment: "sand", Region: "eastus2"})

			require.Error(t, err)
			assert.Empty(t, r.provider.requests, "ni vista previa")
			assert.Empty(t, r.admin.drafts, "ni conexión abierta en el portal")
			assert.NotContains(t, r.log, "account", "ni se consulta la sesión de la nube")
		})
	}
}
