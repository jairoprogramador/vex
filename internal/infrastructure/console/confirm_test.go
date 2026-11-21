package console

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jairoprogramador/vex/internal/application"
)

func TestConfirmer_SoloUnSiExplicitoConfirma(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"s\n", true}, {"S\n", true}, {"si\n", true}, {"sí\n", true}, {"y\n", true}, {"YES\n", true}, {"  s  \n", true},
		{"n\n", false}, {"no\n", false}, {"\n", false}, {"quizá\n", false}, {"", false},
	}
	for _, tt := range tests {
		t.Run(strings.TrimSpace(tt.input), func(t *testing.T) {
			var out bytes.Buffer

			got, err := NewConfirmerWith(strings.NewReader(tt.input), &out, true).Confirm("¿Continuar?")

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, "¿Continuar? [s/N] ", out.String(), "la pregunta dice cuál es la respuesta por defecto")
		})
	}
}

func TestConfirmer_SinTerminalInteractivaNoEsperaNiPregunta(t *testing.T) {
	var out bytes.Buffer

	got, err := NewConfirmerWith(strings.NewReader("s\n"), &out, false).Confirm("¿Continuar?")

	assert.False(t, got, "ni aunque haya un «s» esperando: sin terminal no se confirma por accidente")
	assert.ErrorIs(t, err, ErrNotInteractive)
	assert.Empty(t, out.String())
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("stdin roto") }

func TestConfirmer_UnErrorDeLecturaSePropaga(t *testing.T) {
	_, err := NewConfirmerWith(failingReader{}, &bytes.Buffer{}, true).Confirm("¿Continuar?")

	assert.ErrorContains(t, err, "stdin roto")
}

func TestConfirmer_ConfirmTypedDevuelveLoTecleado(t *testing.T) {
	var out bytes.Buffer

	typed, err := NewConfirmerWith(strings.NewReader("  sand \n"), &out, true).ConfirmTyped("Hay permisos resaltados.", "sand")

	require.NoError(t, err)
	assert.Equal(t, "sand", typed)
	assert.Equal(t, "Hay permisos resaltados.\nEscribe \"sand\" para aceptar: ", out.String())

	typed, err = NewConfirmerWith(strings.NewReader(""), &out, true).ConfirmTyped("x", "sand")
	require.NoError(t, err)
	assert.Empty(t, typed, "sin respuesta no se acepta nada")

	_, err = NewConfirmerWith(strings.NewReader("sand\n"), &out, false).ConfirmTyped("x", "sand")
	assert.ErrorIs(t, err, ErrNotInteractive)
}

// M6 de la revisión de seguridad.
func TestConfirmer_ApprovePortalChanges(t *testing.T) {
	changes := []application.ProjectChange{
		{Field: "pipeline.url", Current: "https://github.com/local/pipe", Proposed: "https://github.com/atac\x1b[2Kante/pipe"},
		{Field: "runtime.image", Current: "local/runtime:v1", Proposed: "otro/runtime:v9"},
	}
	t.Run("enseña la diferencia y pregunta", func(t *testing.T) {
		var out bytes.Buffer

		got, err := NewConfirmerWith(strings.NewReader("s\n"), &out, true).ApprovePortalChanges(changes)

		require.NoError(t, err)
		assert.True(t, got)
		assert.Contains(t, out.String(), "pipeline.url: https://github.com/local/pipe → https://github.com/atac")
		assert.Contains(t, out.String(), "runtime.image: local/runtime:v1 → otro/runtime:v9")
		assert.NotContains(t, out.String(), "\x1b", "lo que dice el portal no lleva secuencias de terminal")
	})
	t.Run("la respuesta por defecto es no", func(t *testing.T) {
		got, err := NewConfirmerWith(strings.NewReader("\n"), &bytes.Buffer{}, true).ApprovePortalChanges(changes)

		require.NoError(t, err)
		assert.False(t, got)
	})
	t.Run("sin terminal no pregunta ni acepta", func(t *testing.T) {
		var out bytes.Buffer

		got, err := NewConfirmerWith(strings.NewReader("s\n"), &out, false).ApprovePortalChanges(changes)

		require.NoError(t, err)
		assert.False(t, got)
		assert.Empty(t, out.String())
	})
}
