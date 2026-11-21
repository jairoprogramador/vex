package application

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Los vectores son los del backend: los dos lados nombran igual lo que crean en la nube.
func TestNombresCompartidosConElBackend(t *testing.T) {
	raw, err := os.ReadFile("../infrastructure/portalclient/testdata/contract/slug-vectors.json")
	require.NoError(t, err)
	var vectors struct {
		Cases []struct {
			Name         string `json:"name"`
			ProjectID    string `json:"project_id"`
			Env          string `json:"env"`
			ConnectionID string `json:"connection_id"`
			Slug         string `json:"slug"`
			Deployment   string `json:"deployment"`
			Subject      string `json:"subject"`
		} `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(raw, &vectors))
	require.NotEmpty(t, vectors.Cases)

	for _, c := range vectors.Cases {
		t.Run(c.Name, func(t *testing.T) {
			slug := ProjectSlug(c.Name, c.ProjectID)
			assert.Equal(t, c.Slug, slug)
			// El backend no acota el nombre (no lo usa: solo lo documenta); la nube admite 64 caracteres.
			assert.Equal(t, c.Deployment, BootstrapOperationName(slug, c.Env))
			subject, err := ConnectionSubject(c.ProjectID, c.Env, c.ConnectionID)
			require.NoError(t, err)
			assert.Equal(t, c.Subject, subject)
		})
	}
}

func TestConnectionSubject_RechazaLoQueNoTieneFormato(t *testing.T) {
	const id = "b5a1c2d4-0000-4000-8000-0000000000b1"
	for name, args := range map[string][3]string{
		"proyecto":                {"seed-id", "sand", id},
		"ambiente con dos puntos": {id, "sand:c:otro", id},
		"ambiente en mayúsculas":  {id, "Sand", id},
		"conexión":                {id, "sand", "x"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ConnectionSubject(args[0], args[1], args[2])
			assert.Error(t, err)
		})
	}
}

func TestConnectionRejectedError_DiceQueHacer(t *testing.T) {
	tests := map[string]string{
		"connection_exists":      "vuelve a ejecutar `vex bootstrap sand`",
		"connection_in_progress": "está conectando este ambiente ahora mismo",
		"not_project_admin":      "solo quien administra el proyecto",
		"environment_not_found":  "`vex envs`",
		"no_active_pipeline":     "`vex envs`",
		"bootstrap_mismatch":     "vuelve a ejecutar `vex bootstrap sand`",
		"not_creator":            "la empezó otra persona",
		"not_found":              "no encuentra esa conexión",
		"contract_not_supported": "no admite el contrato",
		"pending_expired":        "vuelve a ejecutar `vex bootstrap sand`",
		"algo_nuevo":             "(algo_nuevo)",
	}
	for code, want := range tests {
		err := forEnvironment(&ConnectionRejectedError{Code: code}, "sand")
		assert.ErrorContains(t, err, want, code)
	}
	withField := &ConnectionRejectedError{Code: "invalid_param", Field: "outputs.params.resource_group"}
	assert.ErrorContains(t, withField, "outputs.params.resource_group: invalid_param")
}

func TestBootstrapOperationName_AcotaSinPerderLaIdentidadDelAmbiente(t *testing.T) {
	const slug = "servicio-x-y-deadbe"
	long := strings.Repeat("a", 40)

	t.Run("lo que cabe no cambia", func(t *testing.T) {
		exact := strings.Repeat("e", maxOperationName-len("vex-bootstrap-"+slug+"-"))
		assert.Equal(t, "vex-bootstrap-"+slug+"-"+exact, BootstrapOperationName(slug, exact))
		assert.Equal(t, "vex-bootstrap-"+slug+"-sand", BootstrapOperationName(slug, "sand"))
	})
	t.Run("lo que no cabe queda en el límite y es estable", func(t *testing.T) {
		name := BootstrapOperationName(slug, long)
		assert.Len(t, name, maxOperationName)
		assert.Equal(t, name, BootstrapOperationName(slug, long), "bootstrap, --check y disconnect derivan el mismo")
		assert.True(t, strings.HasPrefix(name, "vex-bootstrap-"+slug+"-aaaa"))
		assert.Regexp(t, `-[0-9a-f]{6}$`, name)
	})
	t.Run("dos ambientes largos que empiezan igual no chocan", func(t *testing.T) {
		assert.NotEqual(t, BootstrapOperationName(slug, long+"-uno"), BootstrapOperationName(slug, long+"-dos"))
	})
	t.Run("el recorte no deja guiones dobles", func(t *testing.T) {
		name := BootstrapOperationName(slug, strings.Repeat("a", 23)+"-"+strings.Repeat("b", 30))
		assert.NotContains(t, name, "--")
		assert.LessOrEqual(t, len(name), maxOperationName)
	})
}
