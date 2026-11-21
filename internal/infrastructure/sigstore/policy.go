// Package sigstore verifica firmas sin llaves (Sigstore): la del catálogo de plantillas y la de las imágenes de
// runtime. La política —qué identidad se exige— vive aquí; el resto de la CLI solo conoce los puertos.
package sigstore

const (
	// OIDCIssuer es quien emite la identidad de un workflow de GitHub Actions.
	OIDCIssuer = "https://token.actions.githubusercontent.com"

	// RuntimeIdentityRegexp: una imagen de runtime la firma el workflow docker-publish.yml de un repo
	// vex-runtime-* de la organización, desde un tag de versión. Es la misma regla que usa el catálogo al
	// publicarse (scripts/validate-templates.sh del template store). Además, cada imagen la firma SU repositorio:
	// el que se llama como ella (ver ImageVerifier). Los ids numéricos que también se exigen están en pins.go.
	RuntimeIdentityRegexp = `^https://github\.com/jairoprogramador/vex-runtime-[a-z0-9-]+/\.github/workflows/docker-publish\.yml@refs/tags/v[0-9]+\.[0-9]+\.[0-9]+$`

	// CatalogIdentityRegexp: el catálogo lo firma publish-templates.yml del template store, desde un tag
	// vAAAA.MM.DD-N.
	CatalogIdentityRegexp = `^https://github\.com/jairoprogramador/vex-template-store/\.github/workflows/publish-templates\.yml@refs/tags/v[0-9]{4}\.[0-9]{2}\.[0-9]{2}-[0-9]+$`

	// BundleArtifactType es el tipo con que cosign guarda en el registro la firma de una imagen.
	BundleArtifactType = "application/vnd.dev.sigstore.bundle.v0.3+json"
)
