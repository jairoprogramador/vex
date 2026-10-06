package protocol

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriter_EscribeUnaLineaCompactaSinEscaparHTML(t *testing.T) {
	var buf bytes.Buffer
	req := NewRequest("1", MethodIntentar, PeticionDeIntento{
		Version:   LanguageVersion,
		Ambiente:  "sand",
		HastaPaso: "test",
		Metadatos: Metadatos{ProjectName: "a<b>&c"},
	}, map[string]string{"TOKEN": "x"})

	require.NoError(t, NewWriter(&buf).Write(req))

	out := buf.String()
	assert.True(t, strings.HasSuffix(out, "\n"))
	assert.Equal(t, 1, strings.Count(out, "\n"))
	assert.Contains(t, out, `"method":"intentar"`)
	assert.Contains(t, out, `"Version":"1"`, "los params van en PascalCase")
	assert.Contains(t, out, `"entorno":{"TOKEN":"x"}`)
	assert.Contains(t, out, `a<b>&c`)
}

func TestWriter_CancelarNoLlevaIdNiParams(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, NewWriter(&buf).Write(NewCancel()))
	assert.JSONEq(t, `{"jsonrpc":"2.0","method":"cancelar"}`, buf.String())
}

func TestReader_ProgresoSeguidoDeRespuesta(t *testing.T) {
	input := `{"jsonrpc":"2.0","method":"progreso","params":{"evento":"paso_iniciado","intento":"i1","paso":"test"}}` + "\n" +
		`{"jsonrpc":"2.0","id":"1","result":{"Intento":"i1","Estado":"exitoso","Detalle":{"Tiempo":"1s","Pasos":[{"Nombre":"test","Estado":"ejecutado"}]}}}` + "\n"
	r := NewReader(strings.NewReader(input))

	first, err := r.Read()
	require.NoError(t, err)
	require.True(t, first.IsNotification())
	progress, err := DecodeProgress(first)
	require.NoError(t, err)
	assert.Equal(t, Progreso{Evento: EventoPasoIniciado, Intento: "i1", Paso: "test"}, progress)

	last, err := r.Read()
	require.NoError(t, err)
	assert.False(t, last.IsNotification())
	var result Resultado
	require.NoError(t, DecodeResult(last, &result))
	assert.Equal(t, EstadoExitoso, result.Estado)
	assert.Equal(t, []Paso{{Nombre: "test", Estado: PasoEjecutado}}, result.Detalle.Pasos)

	_, err = r.Read()
	assert.ErrorIs(t, err, io.EOF)
}

func TestReader_UltimaLineaSinSaltoYLineasVacias(t *testing.T) {
	r := NewReader(strings.NewReader("\n\r\n" + `{"jsonrpc":"2.0","id":"1","result":{}}`))

	msg, err := r.Read()
	require.NoError(t, err)
	assert.Equal(t, `"1"`, string(msg.ID))

	_, err = r.Read()
	assert.ErrorIs(t, err, io.EOF)
}

func TestReader_AceptaLineasMayoresQueElLimiteDeScanner(t *testing.T) {
	big := strings.Repeat("x", 512*1024)
	line := `{"jsonrpc":"2.0","id":"1","result":{"Texto":"` + big + `"}}` + "\n"

	msg, err := NewReader(strings.NewReader(line)).Read()
	require.NoError(t, err)

	var out struct{ Texto string }
	require.NoError(t, DecodeResult(msg, &out))
	assert.Len(t, out.Texto, len(big))
}

func TestReader_LineaIlegible(t *testing.T) {
	_, err := NewReader(strings.NewReader("no es json\n")).Read()
	assert.Error(t, err)
}

func TestDecodeResult_ErrorConIdNullNoEsNotificacion(t *testing.T) {
	line := `{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"mala","data":{"tipo":"peticion_invalida"}}}` + "\n"
	msg, err := NewReader(strings.NewReader(line)).Read()
	require.NoError(t, err)
	assert.False(t, msg.IsNotification())

	err = DecodeResult(msg, &Resultado{})

	var engineErr *EngineError
	require.True(t, errors.As(err, &engineErr))
	assert.Equal(t, -32600, engineErr.Code)
	assert.ErrorIs(t, err, ErrPeticionInvalida)
	assert.NotErrorIs(t, err, ErrAmbienteOcupado)
}

func TestDecodeResult_ErroresConDatosAdicionales(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		kind  ErrorKind
		check func(t *testing.T, e *EngineError)
	}{
		{
			name: "ambiente ocupado",
			line: `{"jsonrpc":"2.0","id":"1","error":{"code":-32004,"message":"ocupado","data":{"tipo":"ambiente_ocupado","ambiente":"sand","intento":"i7"}}}`,
			kind: ErrAmbienteOcupado,
			check: func(t *testing.T, e *EngineError) {
				assert.Equal(t, "sand", e.Data.Ambiente)
				assert.Equal(t, "i7", e.Data.Intento)
			},
		},
		{
			name: "rechazado con fallos",
			line: `{"jsonrpc":"2.0","id":"1","error":{"code":-32002,"message":"rechazado","data":{"tipo":"rechazado","fallos":[{"Invariante":"I1","Fichero":"config.yaml","Paso":"test","Ambiente":"","Detalle":"falta"}]}}}`,
			kind: ErrRechazado,
			check: func(t *testing.T, e *EngineError) {
				require.Len(t, e.Data.Fallos, 1)
				assert.Equal(t, "config.yaml", e.Data.Fallos[0].Fichero)
			},
		},
		{
			name: "data ilegible conserva el codigo",
			line: `{"jsonrpc":"2.0","id":"1","error":{"code":-32000,"message":"boom","data":"texto"}}`,
			kind: "",
			check: func(t *testing.T, e *EngineError) {
				assert.Equal(t, -32000, e.Code)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := NewReader(strings.NewReader(tt.line + "\n")).Read()
			require.NoError(t, err)

			err = DecodeResult(msg, &Resultado{})

			var engineErr *EngineError
			require.True(t, errors.As(err, &engineErr))
			assert.Equal(t, tt.kind, engineErr.Kind())
			tt.check(t, engineErr)
		})
	}
}

func TestDecodeProgress_RechazaNotificacionDesconocida(t *testing.T) {
	_, err := DecodeProgress(&Message{Method: "otra"})
	assert.Error(t, err)
}
