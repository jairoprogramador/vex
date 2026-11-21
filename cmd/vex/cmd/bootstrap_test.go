package cmd

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jairoprogramador/vex/internal/application"
)

type fakeBootstrapper struct {
	plan     *application.BootstrapPlan
	planErr  error
	applyErr error
	report   application.CheckReport

	opts       application.BootstrapOptions
	planned    int
	applied    int
	acceptance application.Acceptance
	checked    string
	// exposed son los permisos que la identidad tiene sin que la vista previa los mostrara.
	exposed       []application.PlannedGrant
	exposureTyped string
}

func (f *fakeBootstrapper) Plan(_ context.Context, opts application.BootstrapOptions) (*application.BootstrapPlan, error) {
	f.planned++
	f.opts = opts
	return f.plan, f.planErr
}

func (f *fakeBootstrapper) Apply(_ context.Context, _ *application.BootstrapPlan, acceptance application.Acceptance) (application.BootstrapResult, error) {
	f.applied++
	f.acceptance = acceptance
	if len(f.exposed) > 0 {
		// Como el servicio real: con permisos sin declarar, pregunta antes de crear la confianza.
		f.exposureTyped, _ = acceptance.ConfirmExposure(f.exposed)
	}
	return application.BootstrapResult{Environment: "sand"}, f.applyErr
}

func (f *fakeBootstrapper) Check(_ context.Context, environment string) (application.CheckReport, error) {
	f.checked = environment
	return f.report, nil
}

// fakeAcceptor es la persona (o su ausencia) al otro lado de la terminal.
type fakeAcceptor struct {
	fakeConfirmer
	terminal bool
	typed    string
	expected string
}

func (f *fakeAcceptor) Interactive() bool { return f.terminal }
func (f *fakeAcceptor) ConfirmTyped(_, expected string) (string, error) {
	f.asked++
	f.expected = expected
	return f.typed, f.err
}

func useBootstrapper(t *testing.T, fake *fakeBootstrapper, answers *fakeAcceptor) {
	t.Helper()
	beforeBuild, beforeAcceptor := newBootstrapService, newAcceptor
	newBootstrapService = func() (bootstrapService, error) { return fake, nil }
	newAcceptor = func() acceptor { return answers }
	t.Cleanup(func() {
		newBootstrapService, newAcceptor = beforeBuild, beforeAcceptor
		bootstrapLocation, bootstrapCheck, bootstrapReconnect = "", false, false
	})
}

func planFor(mode application.BootstrapMode, elevated bool) *application.BootstrapPlan {
	plan := &application.BootstrapPlan{Environment: "sand", Mode: mode, Contract: "azure/v1"}
	if elevated {
		plan.Preview.Declared = []application.PlannedGrant{{Identity: "deploy", Role: "Owner", Scope: "toda la suscripción", Elevated: true, Reason: "controla permisos"}}
	}
	return plan
}

func TestBootstrap_SinTerminalNoEmpieza(t *testing.T) {
	fake, answers := &fakeBootstrapper{plan: planFor(application.BootstrapConnect, false)}, &fakeAcceptor{}
	useBootstrapper(t, fake, answers)

	_, stderr, err := run(t, bootstrapCmd, "sand")

	require.ErrorIs(t, err, errBootstrapNeedsTerminal)
	assert.Zero(t, fake.planned, "ni se abre la conexión en el portal ni se toca la nube")
	assert.Contains(t, stderr, "terminal interactiva")
	assert.Contains(t, stderr, "vex bootstrap <ambiente> --check")
}

func TestBootstrap_MuestraElPlanYAplicaSoloSiSeAcepta(t *testing.T) {
	t.Run("sí", func(t *testing.T) {
		fake, answers := &fakeBootstrapper{plan: planFor(application.BootstrapConnect, false)}, &fakeAcceptor{terminal: true}
		answers.answer = true
		useBootstrapper(t, fake, answers)
		bootstrapLocation, bootstrapReconnect = "eastus2", true

		stdout, _, err := run(t, bootstrapCmd, "sand")

		require.NoError(t, err)
		assert.Equal(t, application.BootstrapOptions{Environment: "sand", Region: "eastus2", Reconnect: true}, fake.opts)
		assert.Equal(t, "¿Aplicar este bootstrap en tu nube?", answers.lastAsk)
		assert.True(t, fake.acceptance.Confirmed)
		assert.Empty(t, fake.acceptance.Typed)
		assert.Contains(t, stdout, "Conectar la nube de sand con vex")
		assert.Contains(t, stdout, "Conexión de sand activa y verificada")
	})
	t.Run("no", func(t *testing.T) {
		fake, answers := &fakeBootstrapper{plan: planFor(application.BootstrapConnect, false)}, &fakeAcceptor{terminal: true}
		useBootstrapper(t, fake, answers)

		_, stderr, err := run(t, bootstrapCmd, "sand")

		require.NoError(t, err)
		assert.Zero(t, fake.applied)
		assert.Contains(t, stderr, "No se aplicó nada.")
	})
}

func TestBootstrap_LosPermisosResaltadosSeAceptanPorEscrito(t *testing.T) {
	tests := map[string]struct {
		typed   string
		applied int
	}{
		"teclea el ambiente": {"sand", 1},
		"teclea sí":          {"si", 0},
		"no teclea nada":     {"", 0},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			fake := &fakeBootstrapper{plan: planFor(application.BootstrapReconnect, true)}
			answers := &fakeAcceptor{terminal: true, typed: tt.typed}
			answers.answer = true // un «sí» no basta: ni se pregunta
			useBootstrapper(t, fake, answers)

			_, _, err := run(t, bootstrapCmd, "sand")

			require.NoError(t, err)
			assert.Equal(t, "sand", answers.expected)
			assert.Empty(t, answers.lastAsk, "no se ofrece la confirmación de sí/no")
			assert.Equal(t, tt.applied, fake.applied)
			if tt.applied == 1 {
				assert.Equal(t, "sand", fake.acceptance.Typed)
				assert.False(t, fake.acceptance.Confirmed)
			}
		})
	}
}

func TestBootstrap_SinNadaQueAplicarNoPregunta(t *testing.T) {
	for _, mode := range []application.BootstrapMode{application.BootstrapUpToDate, application.BootstrapResume} {
		fake, answers := &fakeBootstrapper{plan: planFor(mode, false)}, &fakeAcceptor{terminal: true}
		useBootstrapper(t, fake, answers)

		_, _, err := run(t, bootstrapCmd, "sand")

		require.NoError(t, err)
		assert.Zero(t, answers.asked)
		assert.Equal(t, 1, fake.applied)
	}
}

func TestBootstrap_LosErroresSalenConSuMensaje(t *testing.T) {
	fake, answers := &fakeBootstrapper{planErr: application.ErrBootstrapNeedsPortal}, &fakeAcceptor{terminal: true}
	useBootstrapper(t, fake, answers)

	_, stderr, err := run(t, bootstrapCmd, "sand")

	require.ErrorIs(t, err, application.ErrBootstrapNeedsPortal)
	assert.NotEmpty(t, stderr)
}

func TestBootstrapCheck_NoNecesitaTerminalYSaleConErrorSiAlgoFalla(t *testing.T) {
	ok := application.CheckReport{Environment: "sand", Contract: "azure/v1", Items: []application.CheckItem{{Status: application.CheckOK, Text: "La huella coincide"}}}

	t.Run("todo en orden", func(t *testing.T) {
		fake, answers := &fakeBootstrapper{report: ok}, &fakeAcceptor{}
		useBootstrapper(t, fake, answers)
		bootstrapCheck = true

		stdout, _, err := run(t, bootstrapCmd, "sand")

		require.NoError(t, err)
		assert.Equal(t, "sand", fake.checked)
		assert.Zero(t, fake.planned+fake.applied, "auditar no planea ni aplica")
		assert.Contains(t, stdout, "Todo en orden. No se cambió nada.")
	})
	t.Run("con hallazgos", func(t *testing.T) {
		failed := ok
		failed.Items = append(failed.Items, application.CheckItem{Status: application.CheckFailed, Text: "Falta la credencial federada vex-oidc"})
		fake, answers := &fakeBootstrapper{report: failed}, &fakeAcceptor{}
		useBootstrapper(t, fake, answers)
		bootstrapCheck = true

		stdout, _, err := run(t, bootstrapCmd, "sand")

		require.ErrorIs(t, err, application.ErrCheckFoundProblems, "el comando sale distinto de cero")
		assert.Contains(t, stdout, "Falta la credencial federada vex-oidc")
	})
}

func TestBootstrap_CheckNoSeCombinaConLoQueCambia(t *testing.T) {
	groups := bootstrapCmd.Flags().Lookup("check").Annotations["cobra_annotation_mutually_exclusive"]

	assert.ElementsMatch(t, []string{"check reconnect", "check location"}, groups)
}

// --- disconnect ---

type fakeDisconnector struct {
	plan    *application.DisconnectPlan
	planErr error
	result  application.DisconnectResult
	runErr  error
	ran     bool
}

func (f *fakeDisconnector) Plan(context.Context, string) (*application.DisconnectPlan, error) {
	return f.plan, f.planErr
}

func (f *fakeDisconnector) Run(context.Context, *application.DisconnectPlan) (application.DisconnectResult, error) {
	f.ran = true
	return f.result, f.runErr
}

func useDisconnector(t *testing.T, fake *fakeDisconnector, confirm *fakeConfirmer) {
	t.Helper()
	beforeBuild, beforeConfirm := newDisconnector, newConfirmer
	newDisconnector = func() (disconnector, error) { return fake, nil }
	newConfirmer = func() confirmer { return confirm }
	t.Cleanup(func() { newDisconnector, newConfirmer, disconnectYes = beforeBuild, beforeConfirm, false })
}

var activePlan = &application.DisconnectPlan{Environment: "sand", Status: "active", ConnectionID: "c0ffee00-0000-4000-8000-0000000000c1"}

func TestDisconnect_AvisaConfirmaYDesconecta(t *testing.T) {
	fake := &fakeDisconnector{plan: activePlan, result: application.DisconnectResult{
		Environment: "sand", TrustRemoved: true, Cleanup: []string{"az group delete --name rg-pagos-sand"},
	}}
	confirm := &fakeConfirmer{answer: true}
	useDisconnector(t, fake, confirm)

	stdout, _, err := run(t, disconnectCmd, "sand")

	require.NoError(t, err)
	assert.Equal(t, "¿Desconectar?", confirm.lastAsk)
	assert.True(t, fake.ran)
	assert.Contains(t, stdout, "el corte no es instantáneo", "se avisa antes de confirmar")
	assert.Contains(t, stdout, "sand desconectado")
	assert.Contains(t, stdout, "az group delete --name rg-pagos-sand")
}

func TestDisconnect_SiSeRechazaNoSeHaceNada(t *testing.T) {
	fake, confirm := &fakeDisconnector{plan: activePlan}, &fakeConfirmer{}
	useDisconnector(t, fake, confirm)

	_, stderr, err := run(t, disconnectCmd, "sand")

	require.NoError(t, err)
	assert.False(t, fake.ran)
	assert.Contains(t, stderr, "No se hizo nada.")
}

func TestDisconnect_ConYNoPregunta(t *testing.T) {
	fake := &fakeDisconnector{plan: activePlan, result: application.DisconnectResult{Environment: "sand", TrustRemoved: true}}
	confirm := &fakeConfirmer{}
	useDisconnector(t, fake, confirm)
	disconnectYes = true

	_, _, err := run(t, disconnectCmd, "sand")

	require.NoError(t, err)
	assert.True(t, fake.ran)
	assert.Zero(t, confirm.asked)
}

func TestDisconnect_SiLaConfianzaSigueEnLaNubeSaleConErrorYLosComandos(t *testing.T) {
	fake := &fakeDisconnector{plan: activePlan, result: application.DisconnectResult{
		Environment: "sand", Pending: "un bloqueo de la nube lo impide y vex no lo quita por su cuenta",
		Manual: []string{"az lock delete --name vex-protect-identities", "az identity federated-credential delete --name vex-oidc"},
	}}
	useDisconnector(t, fake, &fakeConfirmer{answer: true})

	stdout, _, err := run(t, disconnectCmd, "sand")

	require.ErrorIs(t, err, errTrustStillInCloud)
	assert.Contains(t, stdout, "az lock delete --name vex-protect-identities")
	assert.Contains(t, stdout, "az identity federated-credential delete --name vex-oidc")
}

func TestDisconnect_SiElPortalNoRevocaSeDetiene(t *testing.T) {
	fake := &fakeDisconnector{plan: activePlan, runErr: errors.New("el portal no confirmó la revocación; no se tocó nada en la nube")}
	useDisconnector(t, fake, &fakeConfirmer{answer: true})

	stdout, stderr, err := run(t, disconnectCmd, "sand")

	require.Error(t, err)
	assert.NotContains(t, stdout, "desconectado")
	assert.Contains(t, stderr, "no se tocó nada en la nube")
}

func TestDisconnect_SinConexionLoDice(t *testing.T) {
	fake := &fakeDisconnector{planErr: application.ErrNoCloudConnection}
	useDisconnector(t, fake, &fakeConfirmer{})

	_, _, err := run(t, disconnectCmd, "sand")

	require.ErrorIs(t, err, application.ErrNoCloudConnection)
	assert.False(t, fake.ran)
}

func TestBootstrap_LosPermisosQueNadieVioSeMuestranYSeAceptanPorEscrito(t *testing.T) {
	// Hallazgo A1: con la plantilla aplicada, la identidad resulta tener Owner. Se enseña y se pide teclear.
	fake := &fakeBootstrapper{plan: planFor(application.BootstrapConnect, false), exposed: []application.PlannedGrant{
		{Identity: "identidad id-owner", Role: "Owner", Scope: "toda la suscripción", Elevated: true, Reason: "la vista previa no lo mostró"},
	}}
	answers := &fakeAcceptor{terminal: true, typed: "sand"}
	answers.answer = true
	useBootstrapper(t, fake, answers)

	stdout, _, err := run(t, bootstrapCmd, "sand")

	require.NoError(t, err)
	assert.Contains(t, stdout, "LA IDENTIDAD DE DESPLIEGUE TIENE PERMISOS QUE LA VISTA PREVIA NO MOSTRÓ")
	assert.Contains(t, stdout, "identidad id-owner → Owner → toda la suscripción")
	assert.Contains(t, stdout, "RESALTADO")
	assert.Equal(t, "sand", answers.expected, "se pide teclear el ambiente, no un sí")
	assert.Equal(t, "sand", fake.exposureTyped)
}
