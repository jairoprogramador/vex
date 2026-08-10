package application

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	proVos "github.com/jairoprogramador/vex/internal/domain/project/vos"
)

func TestEncodeStateConfig_EsElContratoDelMotorEnBase64(t *testing.T) {
	encoded, err := encodeStateConfig("/vexState")
	require.NoError(t, err)

	raw, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)

	// El motor lo lee con YAML, que acepta este JSON tal cual.
	assert.JSONEq(t, `{"type":"local","local":{"path":"/vexState"}}`, string(raw))
}

func TestEncodeStateConfig_SinRutaNoHayConfiguracion(t *testing.T) {
	_, err := encodeStateConfig("   ")
	require.Error(t, err, "un destino vacío haría que el motor saliera con exit code 2 sin decir por qué")
}

func TestContainerStagingDir(t *testing.T) {
	xdg, err := proVos.NewEnvVar("XDG_STATE_HOME", "/tmp/estado")
	require.NoError(t, err)
	otra, err := proVos.NewEnvVar("JAVA_HOME", "/usr/lib/jvm")
	require.NoError(t, err)

	tests := []struct {
		nombre string
		env    []proVos.EnvVar
		quiero string
	}{
		{
			nombre: "sin XDG_STATE_HOME es el $HOME del usuario del runtime",
			env:    []proVos.EnvVar{otra},
			quiero: "/home/vex/.local/state/vex/staging",
		},
		{
			nombre: "con XDG_STATE_HOME manda la declarada",
			env:    []proVos.EnvVar{otra, xdg},
			quiero: "/tmp/estado/vex/staging",
		},
	}

	for _, tt := range tests {
		t.Run(tt.nombre, func(t *testing.T) {
			assert.Equal(t, tt.quiero, containerStagingDir(tt.env))
		})
	}
}

func TestEnsureStagingOutsideMounts(t *testing.T) {
	// Los dos montajes reales, en el mismo orden en que los pasa el ejecutor.
	montajes := []string{"/vexState", "/vexHome/.vex"}

	enDestino, err := proVos.NewEnvVar("XDG_STATE_HOME", "/vexState/estado")
	require.NoError(t, err)
	enTrabajo, err := proVos.NewEnvVar("XDG_STATE_HOME", "/vexHome/.vex/estado")
	require.NoError(t, err)
	fuera, err := proVos.NewEnvVar("XDG_STATE_HOME", "/tmp/estado")
	require.NoError(t, err)
	vecino, err := proVos.NewEnvVar("XDG_STATE_HOME", "/vexState-otro/estado")
	require.NoError(t, err)

	t.Run("el default del runtime está fuera de los dos por construcción", func(t *testing.T) {
		assert.NoError(t, ensureStagingOutsideMounts(nil, montajes...))
	})

	t.Run("un XDG declarado fuera tampoco molesta", func(t *testing.T) {
		assert.NoError(t, ensureStagingOutsideMounts([]proVos.EnvVar{fuera}, montajes...))
	})

	t.Run("un prefijo que no es un padre no cuenta como dentro", func(t *testing.T) {
		assert.NoError(t, ensureStagingOutsideMounts([]proVos.EnvVar{vecino}, montajes...))
	})

	t.Run("dentro del destino aborta nombrando lo que se pisaría", func(t *testing.T) {
		err := ensureStagingOutsideMounts([]proVos.EnvVar{enDestino}, montajes...)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "/vexState/estado/vex/staging")
		assert.Contains(t, err.Error(), "/vexState")
		assert.Contains(t, err.Error(), "objects/, events/ y ack/")
	})

	t.Run("dentro del área de trabajo también aborta", func(t *testing.T) {
		err := ensureStagingOutsideMounts([]proVos.EnvVar{enTrabajo}, montajes...)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "/vexHome/.vex")
	})

	t.Run("un montaje que ES el área de trabajo", func(t *testing.T) {
		require.Error(t, ensureStagingOutsideMounts([]proVos.EnvVar{enDestino},
			"/vexState/estado/vex/staging"))
	})
}
