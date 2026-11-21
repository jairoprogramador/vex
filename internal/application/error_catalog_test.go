package application

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	arqPor "github.com/jairoprogramador/vex/internal/domain/architecture/ports"
)

// catalogDoc es el documento donde tiene que aparecer cada código del catálogo.
const catalogDoc = "../../docs/solucion-de-problemas.md"

func TestCatalogoEstaDocumentado(t *testing.T) {
	doc, err := os.ReadFile(catalogDoc)
	require.NoError(t, err)
	for _, rule := range ErrorRules() {
		assert.True(t, strings.Contains(string(doc), "`"+rule.Code+"`"), "el código %s no está en docs/solucion-de-problemas.md", rule.Code)
	}
}

func TestTodaReglaTieneAccion(t *testing.T) {
	seen := map[string]bool{}
	for _, rule := range ErrorRules() {
		t.Run(rule.Code, func(t *testing.T) {
			assert.Regexp(t, `^[A-Za-z0-9_]+$`, rule.Code)
			assert.False(t, seen[rule.Code], "código repetido")
			seen[rule.Code] = true
			require.NotNil(t, rule.Example, "toda regla trae un error de ejemplo")

			message, action, ok := rule.Explain(rule.Example)
			require.True(t, ok, "la regla no reconoce su propio ejemplo")
			assert.NotEmpty(t, strings.TrimSpace(message), "falta qué pasó")
			assert.NotEmpty(t, strings.TrimSpace(action), "falta qué hacer")
			assert.NotContains(t, message, "\n→", "el mensaje no lleva la acción dentro")

			// Envuelto, como llega de verdad, lo sigue reconociendo esta regla y no otra anterior.
			explained, ok := ExplainError(fmt.Errorf("al hacer algo: %w", rule.Example))
			require.True(t, ok)
			assert.Equal(t, rule.Code, explained.Code, "otra regla se queda con este error")
			assert.ErrorIs(t, explained, rule.Example)
		})
	}
}

func TestExplainError_LoDesconocidoNoSeInventa(t *testing.T) {
	_, ok := ExplainError(errors.New("algo que nadie catalogó"))
	assert.False(t, ok)
	_, ok = ExplainError(nil)
	assert.False(t, ok)
}

// El código común no sabe de ninguna nube: lo de cada una lo aporta su adaptador (ErrorCatalog).
func TestComunNoMencionaAzure(t *testing.T) {
	source, err := os.ReadFile("error_catalog.go")
	require.NoError(t, err)
	lower := strings.ToLower(string(source))
	for _, word := range []string{"azure", "aadsts", "subscription", "suscripci", "`az "} {
		assert.NotContains(t, lower, word)
	}
}

// Lo que el catálogo muestra es lo que la persona ya conoce; la causa técnica (que puede traer respuestas del
// registro o del portal) no sale.
func TestCatalogoNoMuestraLaCausaTecnica(t *testing.T) {
	const secret = "eyJhbGciOiJSUzI1NiJ9.secreto"
	for _, err := range []error{
		&ImageVerificationError{Image: "acme/runtime", Tag: "1.0", Reason: ErrImageRegistry, Detail: "GET https://reg/token?jwt=" + secret},
		&ImageVerificationError{Image: "acme/runtime", Reason: ErrImageNotSigned, Detail: secret},
		fmt.Errorf("%w: %s", arqPor.ErrCatalogSignature, secret),
		fmt.Errorf("%w: Get https://x/?token=%s", arqPor.ErrCatalogUnavailable, secret),
		&CloudAccessLostError{Environment: "sand", Step: "deploy", Err: fmt.Errorf("%w: %s", ErrCloudTokenDenied, secret)},
	} {
		explained, ok := ExplainError(err)
		require.True(t, ok, "%v", err)
		assert.NotContains(t, explained.Error(), secret)
	}
}

func TestActionableError_Formato(t *testing.T) {
	err := &ActionableError{Code: "x", Message: "Qué pasó.", Action: "Qué hacer"}
	assert.Equal(t, "Qué pasó.\n→ Qué hacer", err.Error())
	assert.Equal(t, "Solo esto.", (&ActionableError{Message: "Solo esto."}).Error())
}

func TestCloudAdapters_ErrorCatalogs(t *testing.T) {
	plain := fakeCloudAdapter{contract: "nube/v1"}
	assert.Empty(t, NewCloudAdapters(plain).ErrorCatalogs(), "un adaptador sin catálogo no aporta nada")
}
