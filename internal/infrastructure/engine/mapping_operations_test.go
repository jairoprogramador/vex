package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jairoprogramador/vex/internal/application"
	"github.com/jairoprogramador/vex/internal/infrastructure/engine/protocol"
)

var at0 = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

func declared(names ...string) []protocol.PasoDeclarado {
	steps := make([]protocol.PasoDeclarado, len(names))
	for i, n := range names {
		steps[i] = protocol.PasoDeclarado{Nombre: n}
	}
	return steps
}

func record(step, kind string, ok bool, seconds int) protocol.RegistroDePaso {
	return protocol.RegistroDePaso{Paso: step, Tipo: kind, Exitoso: ok, Instante: at0.Add(time.Duration(seconds) * time.Second)}
}

func TestToAttemptDetail_QuéPasóEnCadaPaso(t *testing.T) {
	tests := []struct {
		name    string
		attempt protocol.IntentoDeHistorial
		want    []application.StepOutcome
	}{
		{
			name: "exitoso: ejecutado y reutilizado",
			attempt: protocol.IntentoDeHistorial{
				Estado:   protocol.EstadoExitoso,
				Apertura: protocol.Apertura{Pasos: declared("test", "deploy"), HastaPaso: "deploy"},
				Registros: []protocol.RegistroDePaso{
					record("test", protocol.RegistroNoReejecucion, false, 1),
					record("deploy", protocol.RegistroComienzo, false, 2),
					record("deploy", protocol.RegistroFinal, true, 5),
				},
			},
			want: []application.StepOutcome{
				{Name: "test", Status: application.StepReused}, {Name: "deploy", Status: application.StepExecuted},
			},
		},
		{
			name: "fallido: el paso que falló y los que no se alcanzaron",
			attempt: protocol.IntentoDeHistorial{
				Estado:   protocol.EstadoFallido,
				Apertura: protocol.Apertura{Pasos: declared("test", "package", "deploy"), HastaPaso: "deploy"},
				Registros: []protocol.RegistroDePaso{
					record("test", protocol.RegistroComienzo, false, 1), record("test", protocol.RegistroFinal, true, 2),
					record("package", protocol.RegistroComienzo, false, 3), record("package", protocol.RegistroFinal, false, 4),
				},
			},
			want: []application.StepOutcome{
				{Name: "test", Status: application.StepExecuted}, {Name: "package", Status: application.StepFailed},
				{Name: "deploy", Status: application.StepPending},
			},
		},
		{
			name: "un paso que empezó y nunca terminó",
			attempt: protocol.IntentoDeHistorial{
				Estado:    protocol.EstadoFallido,
				Causa:     "interrumpido",
				Apertura:  protocol.Apertura{Pasos: declared("test"), HastaPaso: "test"},
				Registros: []protocol.RegistroDePaso{record("test", protocol.RegistroComienzo, false, 1)},
			},
			want: []application.StepOutcome{{Name: "test", Status: application.StepUnfinished}},
		},
		{
			name: "cancelado: el comando que murió por la cancelación se muestra como cancelado, no como fallido",
			attempt: protocol.IntentoDeHistorial{
				Estado:   protocol.EstadoCancelado,
				Apertura: protocol.Apertura{Pasos: declared("test"), HastaPaso: "test"},
				Registros: []protocol.RegistroDePaso{
					record("test", protocol.RegistroComienzo, false, 1), record("test", protocol.RegistroFinal, false, 2),
				},
			},
			want: []application.StepOutcome{{Name: "test", Status: application.StepCanceled}},
		},
		{
			name: "los pasos posteriores a HastaPaso no cuentan",
			attempt: protocol.IntentoDeHistorial{
				Estado:   protocol.EstadoExitoso,
				Apertura: protocol.Apertura{Pasos: declared("test", "deploy"), HastaPaso: "test"},
				Registros: []protocol.RegistroDePaso{
					record("test", protocol.RegistroComienzo, false, 1), record("test", protocol.RegistroFinal, true, 2),
				},
			},
			want: []application.StepOutcome{{Name: "test", Status: application.StepExecuted}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, toAttemptDetail(tt.attempt).Steps)
		})
	}
}

func TestToAttemptDetail_DatosGenerales(t *testing.T) {
	detail := toAttemptDetail(protocol.IntentoDeHistorial{
		Id: "i1", Instante: at0, Estado: protocol.EstadoFallido, Causa: "error", Destino: "d7", Abandonado: true,
		Apertura:  protocol.Apertura{Ambiente: "sand", Solicitante: "ana", HastaPaso: "test", Pasos: declared("test")},
		Registros: []protocol.RegistroDePaso{record("test", protocol.RegistroComienzo, false, 7)},
	})

	assert.Equal(t, application.AttemptSummary{
		ID: "i1", Environment: "sand", Requester: "ana", UntilStep: "test", StartedAt: at0,
		Status: application.AttemptFailed, Cause: application.CauseError, Abandoned: true,
	}, detail.AttemptSummary)
	assert.True(t, detail.Abandoned)
	assert.Equal(t, "d7", detail.RolledBackTo)
	assert.Equal(t, 7*time.Second, detail.Duration)
}

func TestToSummary_UnIntentoSinDesenlaceEstaEnCurso(t *testing.T) {
	summary := toSummary(protocol.ResumenDeIntento{Id: "i1", Instante: at0})

	assert.True(t, summary.InProgress())
	assert.Equal(t, at0, summary.StartedAt)
	assert.Empty(t, summary.Cause)
}

func TestToCause(t *testing.T) {
	assert.Equal(t, application.CauseInterrupted, toCause("interrumpido"))
	assert.Equal(t, application.CauseError, toCause("error"))
	assert.Equal(t, application.AttemptCause(""), toCause(""))
	assert.Equal(t, application.AttemptCause("nueva"), toCause("nueva"), "una causa futura no se pierde")
}

func TestToCheckResult(t *testing.T) {
	assert.Equal(t, application.CheckResult{Valid: true},
		toCheckResult(protocol.ResultadoDeSimulacion{Estado: protocol.EstadoExitoso}))

	failures := toCheckResult(protocol.ResultadoDeSimulacion{
		Estado: protocol.EstadoFallido,
		Causa: &protocol.CausaDeSimulacion{Fallos: []protocol.Fallo{
			{Invariante: "formato", Fichero: "config.yaml", Paso: "test", Detalle: "no se lee"},
		}},
	})
	assert.False(t, failures.Valid)
	assert.Equal(t, []application.PipelineFailure{
		{Invariant: "formato", File: "config.yaml", Step: "test", Detail: "no se lee"},
	}, failures.Failures)

	missing := toCheckResult(protocol.ResultadoDeSimulacion{
		Estado: protocol.EstadoFallido,
		Causa:  &protocol.CausaDeSimulacion{Faltante: []protocol.Faltante{{Paso: "deploy", Variables: []string{"a", "b"}}}},
	})
	assert.Equal(t, []application.MissingVariables{{Step: "deploy", Variables: []string{"a", "b"}}}, missing.Missing)
}

func TestToDiagnosis(t *testing.T) {
	t.Run("sin referencia y no se atribuye", func(t *testing.T) {
		assert.Equal(t, application.DiagnosisNoReference,
			toDiagnosis(protocol.RespuestaDeDiagnostico{SinDiagnostico: protocol.SinReferencia}).Kind)
		assert.Equal(t, application.DiagnosisNotAttributable,
			toDiagnosis(protocol.RespuestaDeDiagnostico{SinDiagnostico: protocol.NoSeAtribuye}).Kind)
	})

	t.Run("con diagnóstico", func(t *testing.T) {
		d := toDiagnosis(protocol.RespuestaDeDiagnostico{
			Ambiente:       "sand",
			IntentoExitoso: &protocol.IntentoDeReferencia{Id: "ok", Fecha: at0},
			IntentoFallido: &protocol.IntentoQueFalla{Id: "ko", Fecha: at0.Add(time.Hour), CantidadDeIntentos: 3},
			Sustento: &protocol.Sustento{
				Codigo:        &protocol.PasosCambiados{Pasos: []string{"build"}},
				Instrucciones: &protocol.PasosCambiados{Pasos: []string{"deploy"}},
				Variables: &protocol.VariablesCambiadas{
					DeclaradasCambiadas: []protocol.VariableDeUnPaso{{Paso: "deploy", Nombre: "db_url"}},
				},
			},
		})

		assert.Equal(t, application.DiagnosisFound, d.Kind)
		assert.Equal(t, "ok", d.Reference.ID)
		assert.Equal(t, "ko", d.Failed.ID)
		assert.Equal(t, 3, d.AttemptsSince)
		assert.Equal(t, []string{"build"}, d.Changes.Code)
		assert.Equal(t, []string{"deploy"}, d.Changes.Instructions)
		assert.Equal(t, []application.VariableRef{{Step: "deploy", Name: "db_url"}}, d.Changes.DeclaredVars)
		assert.False(t, d.Changes.Nothing())
	})

	t.Run("un sustento vacío es que no cambió nada", func(t *testing.T) {
		d := toDiagnosis(protocol.RespuestaDeDiagnostico{
			IntentoExitoso: &protocol.IntentoDeReferencia{Id: "ok"}, IntentoFallido: &protocol.IntentoQueFalla{Id: "ko"},
			Sustento: &protocol.Sustento{},
		})

		require.Equal(t, application.DiagnosisFound, d.Kind)
		assert.True(t, d.Changes.Nothing())
	})
}

func TestTranslateError_ParametroInvalidoDiceElCampoYElValor(t *testing.T) {
	err := translateError(protocol.DecodeResult(&protocol.Message{Error: &protocol.RPCError{
		Code: -32602, Message: "inválido", Data: []byte(`{"tipo":"parametros_invalidos","campo":"Ambiente","valor":"nope"}`),
	}}, &struct{}{}), "")

	var engineErr *application.EngineError
	require.ErrorAs(t, err, &engineErr)
	assert.Equal(t, application.EngineInvalidParams, engineErr.Kind)
	assert.Equal(t, "Ambiente", engineErr.Field)
	assert.Equal(t, "nope", engineErr.Value)
}

func TestTranslateError_OperacionDesconocidaEsUnaImagenAntigua(t *testing.T) {
	err := translateError(protocol.DecodeResult(&protocol.Message{Error: &protocol.RPCError{
		Code: -32601, Message: "operación desconocida", Data: []byte(`{"tipo":"operacion_desconocida"}`),
	}}, &struct{}{}), "")

	var engineErr *application.EngineError
	require.ErrorAs(t, err, &engineErr)
	assert.Equal(t, application.EngineUnknownOperation, engineErr.Kind)
}
