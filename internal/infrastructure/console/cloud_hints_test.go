package console

import (
	"bytes"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/jairoprogramador/vex/internal/application"
	"github.com/jairoprogramador/vex/internal/infrastructure/cloud/azure"
	"github.com/jairoprogramador/vex/internal/infrastructure/portalauth"
)

// Las instrucciones son las del adaptador azure/v1; los textos son salidas reales de az y de terraform.
var azureHints = azure.New().ErrorHints()

func TestCloudHints_ErroresDeAzure(t *testing.T) {
	tests := map[string]struct {
		output string
		want   string
	}{
		"confianza eliminada": {
			"ERROR: AADSTS70021: No matching federated identity record found for presented assertion. Assertion Issuer: 'https://oidc.vex.dev'.",
			"La confianza de vex en Azure para este ambiente fue modificada o eliminada. Revísala con `vex bootstrap sand --check`",
		},
		"token vencido": {
			"AADSTS700024: Client assertion is not within its valid time range. Current time: 2026-11-20T12:20:00Z",
			"El token de despliegue venció (¿la máquina se suspendió?). Vuelve a ejecutar.",
		},
		"permisos de ARM": {
			`Error: (AuthorizationFailed) The client '4444' with object id '4444' does not have authorization to perform action 'Microsoft.App/containerApps/write'`,
			"Azure rechazó la operación por permisos. Si acabas de ejecutar `vex bootstrap`, los permisos pueden tardar en propagarse: reintenta en 1 minuto. Si persiste, revisa la conexión con `vex bootstrap sand --check`",
		},
		"identidad no validada": {
			"AADSTS700211: No matching federated identity record found for presented assertion issuer",
			"Azure no pudo validar la identidad de vex para este ambiente. Revísala con `vex bootstrap sand --check`",
		},
		"el token no apareció": {
			"vex-azure-login: el token no apareció en /vex/credenciales/oidc.jwt tras 15 s",
			"No se obtuvo el token de Azure para este paso. Revisa tu conexión a Internet y vuelve a ejecutar; si persiste, vex why muestra el detalle.",
		},
		"permisos del directorio": {
			"vex-azure-login: sin permiso para leer /vex/credenciales/oidc.jwt",
			"El contenedor no puede leer el token (permisos del directorio de credenciales). Ver docs/solucion-de-problemas.md › «Linux: permisos del token».",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, []string{tt.want}, CloudHints(tt.output, azureHints, "sand"))
		})
	}
}

func TestCloudHints(t *testing.T) {
	t.Run("texto sin ningún error conocido", func(t *testing.T) {
		assert.Empty(t, CloudHints("BUILD FAILURE: tests failed", azureHints, "sand"))
	})
	t.Run("AADSTS700211 no se confunde con AADSTS70021", func(t *testing.T) {
		hints := CloudHints("AADSTS700211: issuer mismatch", azureHints, "sand")
		assert.Len(t, hints, 1)
		assert.Contains(t, hints[0], "no pudo validar la identidad")
	})
	t.Run("dos errores en un texto dan dos instrucciones", func(t *testing.T) {
		hints := CloudHints("AADSTS700024 ...\nAuthorizationFailed ...\nAuthorizationFailed otra vez", azureHints, "sand")
		assert.Len(t, hints, 2)
	})
	t.Run("dos patrones con el mismo consejo no lo repiten", func(t *testing.T) {
		same := []application.ErrorHint{
			{Pattern: regexp.MustCompile("A"), Message: "haz esto"},
			{Pattern: regexp.MustCompile("B"), Message: "haz esto"},
		}
		assert.Equal(t, []string{"haz esto"}, CloudHints("A y B", same, "sand"))
	})
	t.Run("sin instrucciones", func(t *testing.T) {
		assert.Empty(t, CloudHints("AADSTS70021", nil, "sand"))
	})
	t.Run("sin ambiente conocido lo deja por rellenar", func(t *testing.T) {
		assert.Contains(t, CloudHints("AADSTS70021: x", azureHints, "")[0], "vex bootstrap <ambiente> --check")
	})
}

func cloudNotice() application.CloudNotice {
	return application.CloudNotice{
		Environment: "sand", Contract: "azure/v1", ConnectionID: "5b0c8a52-3f0e-4d55-9a6e-0f6d0e1c2a11", Hints: azureHints,
	}
}

func TestPresenter_AnunciaLaConexionDeNubeSinDatosDeLaNube(t *testing.T) {
	p, out, _ := newTestPresenter(t)

	p.CloudConnected(cloudNotice())

	assert.Equal(t, "• Conexión de nube activa (azure/v1 · 5b0c8a52) · detalle: vex bootstrap sand --check\n", out.String())
}

func TestPresenter_MuestraLoQueElPipelinePidioMostrar(t *testing.T) {
	result := application.AttemptResult{
		AttemptID: "i1", Status: application.AttemptSucceeded, Duration: "2m50s", DeploymentID: "d7",
		Elapsed: 8*time.Minute + 2*time.Second + 400*time.Millisecond,
		Visible: []application.VisibleOutput{
			{Name: "app_url", Description: "URL de la app", Value: "https://pagos-api.example.io"},
			{Name: "region", Value: "brazilsouth"},
		},
	}

	t.Run("con las credenciales del portal", func(t *testing.T) {
		p, out, _ := newTestPresenter(t)
		p.plain = false
		p.cloud = &application.CloudNotice{}

		p.Result(result)

		assert.Equal(t, "\n✔ Intento i1: exitoso (2m50s)\n  Despliegue: d7\n"+
			"  🚀 https://pagos-api.example.io\n  region: brazilsouth\n"+
			"  Listo en 8m02s · sin secretos guardados\n", out.String())
	})
	t.Run("sin color no hay emoji", func(t *testing.T) {
		p, out, _ := newTestPresenter(t)
		p.cloud = &application.CloudNotice{}

		p.Result(result)

		assert.Contains(t, out.String(), "  -> https://pagos-api.example.io\n")
		assert.NotContains(t, out.String(), "🚀")
	})
	t.Run("con las credenciales del usuario no se presume de secretos", func(t *testing.T) {
		p, out, _ := newTestPresenter(t)

		p.Result(result)

		assert.Contains(t, out.String(), "-> https://pagos-api.example.io")
		assert.NotContains(t, out.String(), "sin secretos guardados")
	})
	t.Run("un intento fallido no muestra nada de eso", func(t *testing.T) {
		p, out, _ := newTestPresenter(t)
		p.cloud = &application.CloudNotice{}
		failed := result
		failed.Status = application.AttemptFailed

		p.Result(failed)

		assert.Equal(t, "\n✘ Intento i1: fallido (2m50s)\n  Despliegue: d7\n", out.String())
	})
	t.Run("un valor no puede escribir secuencias de control en la terminal", func(t *testing.T) {
		p, out, _ := newTestPresenter(t)

		p.Result(application.AttemptResult{Status: application.AttemptSucceeded, Visible: []application.VisibleOutput{
			{Name: "x\x1b[2J", Value: "https://a.example\x1b]0;título\x07\nlínea falsa"},
		}})

		assert.NotContains(t, out.String(), "\x1b")
		assert.NotContains(t, out.String(), "\x07")
		assert.NotContains(t, out.String(), "\nlínea falsa")
	})
}

func TestElapsed(t *testing.T) {
	tests := map[time.Duration]string{
		0:                             "0s",
		42 * time.Second:              "42s",
		8*time.Minute + 2*time.Second: "8m02s",
		time.Hour + 5*time.Minute + 9*time.Second: "1h05m09s",
		59*time.Second + 600*time.Millisecond:     "1m00s",
	}
	for d, want := range tests {
		assert.Equal(t, want, elapsed(d))
	}
}

func TestPresenter_UnFalloDeNubeLlevaSuInstruccionTrasLaSalida(t *testing.T) {
	p, _, errOut := newTestPresenter(t)
	p.cloud = &application.CloudNotice{Environment: "sand", Hints: azureHints}

	p.Failure("01a113d0-7845-79ee-9381-be4f9efac5ab", []application.CommandOutput{
		{Step: "supply", Command: "vex-azure-login", Text: "ERROR: AADSTS70021: No matching federated identity record found\n"},
		{Step: "supply", Command: "terraform apply", Text: "AADSTS70021 otra vez\n"},
	})

	assert.Equal(t, "\n✘ supply › vex-azure-login\nERROR: AADSTS70021: No matching federated identity record found\n"+
		"→ La confianza de vex en Azure para este ambiente fue modificada o eliminada. Revísala con `vex bootstrap sand --check`\n"+
		"\n✘ supply › terraform apply\nAADSTS70021 otra vez\n"+
		"→ vex why efac5ab · vex log efac5ab --failed\n", errOut.String(), "la misma instrucción no se repite")
}

func TestPresenter_SinPortalNoHayInstruccionesDeNube(t *testing.T) {
	p, _, errOut := newTestPresenter(t)

	p.Failure("", []application.CommandOutput{{Step: "supply", Command: "az login", Text: "AADSTS70021"}})

	assert.Equal(t, "\n✘ supply › az login\nAADSTS70021\n", errOut.String())
}

func TestViews_Logs_LosFallidosLlevanLasInstruccionesDeNube(t *testing.T) {
	logs := application.AttemptLogs{AttemptID: idFailed, Outputs: []application.CommandOutput{
		{Step: "deploy", Command: "terraform apply", Text: "Error: AuthorizationFailed\n"},
	}}

	t.Run("con --failed", func(t *testing.T) {
		var out bytes.Buffer
		NewViews(&out).WithCloudHints(azureHints).Logs(logs, true)

		assert.Contains(t, out.String(), "  Error: AuthorizationFailed\n  → Azure rechazó la operación por permisos.")
		assert.Contains(t, out.String(), "vex bootstrap <ambiente> --check")
	})
	t.Run("la salida completa no se anota", func(t *testing.T) {
		var out bytes.Buffer
		NewViews(&out).WithCloudHints(azureHints).Logs(logs, false)

		assert.NotContains(t, out.String(), "→")
	})
}

func TestDescribeError_MotorSinSoporteDelContexto(t *testing.T) {
	message := DescribeError(&application.EngineError{Kind: application.EngineContextUnsupported, Message: "campo desconocido"})

	assert.Contains(t, message, "sin soporte para credenciales de nube del portal")
	assert.Contains(t, message, "runtime.image")
}

func TestDescribeError_InicioDeSesionFallido(t *testing.T) {
	denied := fmt.Errorf("iniciar sesión en vex: device flow: %w", portalauth.ErrAccessDenied)
	expired := fmt.Errorf("iniciar sesión en vex: device flow: %w", portalauth.ErrDeviceCodeExpired)

	assert.Contains(t, DescribeError(denied), "rechazó el inicio de sesión")
	assert.Contains(t, DescribeError(expired), "venció antes de que lo aprobaras")
}
