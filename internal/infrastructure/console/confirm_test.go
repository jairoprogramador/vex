package console

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
