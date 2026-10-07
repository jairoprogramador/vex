package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jairoprogramador/vex/internal/application"
	"github.com/jairoprogramador/vex/internal/infrastructure/console"
)

type fakeRollbacker struct {
	plan    application.RollbackPlan
	planErr error
	runErr  error

	planRef string
	ran     bool
}

func (f *fakeRollbacker) Plan(ref string) (application.RollbackPlan, error) {
	f.planRef = ref
	return f.plan, f.planErr
}

func (f *fakeRollbacker) Run(context.Context, application.RollbackPlan) error {
	f.ran = true
	return f.runErr
}

func useRollbacker(t *testing.T, fake *fakeRollbacker, confirm *fakeConfirmer) {
	t.Helper()
	beforeBuild, beforeConfirm := newRollbacker, newConfirmer
	newRollbacker = func() (rollbacker, error) { return fake, nil }
	newConfirmer = func() confirmer { return confirm }
	t.Cleanup(func() { newRollbacker, newConfirmer, yesFlag = beforeBuild, beforeConfirm, false })
}

var rollbackPlan = application.RollbackPlan{
	DeploymentID: "01a113d0-7845-79ee-9381-be4f974d0dee", Environment: "prod", DeployedAt: time.Now().Add(-48 * time.Hour),
}

func TestRollback_AvisaDeQueSeEjecutaTodoYSoloEjecutaSiSeConfirma(t *testing.T) {
	fake, confirm := &fakeRollbacker{plan: rollbackPlan}, &fakeConfirmer{answer: true}
	useRollbacker(t, fake, confirm)

	stdout, _, err := run(t, rollbackCmd, "f974d0dee")

	require.NoError(t, err)
	assert.Equal(t, "f974d0dee", fake.planRef)
	assert.True(t, fake.ran)
	assert.Equal(t, "¿Continuar?", confirm.lastAsk)
	assert.Contains(t, stdout, "Volverás a desplegar en prod los mismos commits del despliegue 74d0dee")
	assert.Contains(t, stdout, "Se ejecutarán TODOS los pasos del pipeline.", "la persona sabe qué va a pasar antes de confirmar")
}

func TestRollback_SiSeRechazaNoSeEjecutaNada(t *testing.T) {
	fake, confirm := &fakeRollbacker{plan: rollbackPlan}, &fakeConfirmer{answer: false}
	useRollbacker(t, fake, confirm)

	_, stderr, err := run(t, rollbackCmd, "f974d0dee")

	require.NoError(t, err)
	assert.False(t, fake.ran)
	assert.Contains(t, stderr, "No se hizo nada.")
}

func TestRollback_ConYNoPregunta(t *testing.T) {
	fake, confirm := &fakeRollbacker{plan: rollbackPlan}, &fakeConfirmer{}
	useRollbacker(t, fake, confirm)
	require.NoError(t, rollbackCmd.Flags().Set("yes", "true"))

	_, _, err := run(t, rollbackCmd, "f974d0dee")

	require.NoError(t, err)
	assert.True(t, fake.ran)
	assert.Zero(t, confirm.asked)
}

func TestRollback_SinTerminalExigeY(t *testing.T) {
	fake, confirm := &fakeRollbacker{plan: rollbackPlan}, &fakeConfirmer{err: console.ErrNotInteractive}
	useRollbacker(t, fake, confirm)

	_, stderr, err := run(t, rollbackCmd, "f974d0dee")

	require.ErrorIs(t, err, console.ErrNotInteractive)
	assert.False(t, fake.ran, "ejecutar todo el pipeline en prod sin poder confirmar no es una opción")
	assert.Contains(t, stderr, "Añade -y")
}

func TestRollback_UnPlanQueFallaNoPideConfirmacion(t *testing.T) {
	fake, confirm := &fakeRollbacker{planErr: application.ErrUnknownID}, &fakeConfirmer{answer: true}
	useRollbacker(t, fake, confirm)

	_, stderr, err := run(t, rollbackCmd, "zzzzzzz")

	require.ErrorIs(t, err, application.ErrUnknownID)
	assert.Zero(t, confirm.asked)
	assert.Contains(t, stderr, "No encuentro ese id")
}

func TestRollback_UnRollbackFallidoOCanceladoSaleConSuCodigo(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code int
	}{
		{"fallido", application.ErrAttemptFailed, 1},
		{"cancelado con Ctrl+C", application.ErrAttemptCanceled, 130},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useRollbacker(t, &fakeRollbacker{plan: rollbackPlan, runErr: tt.err}, &fakeConfirmer{answer: true})

			_, _, err := run(t, rollbackCmd, "f974d0dee")

			require.Error(t, err)
			assert.Equal(t, tt.code, exitCode(err))
		})
	}
}

func TestRollback_UnErrorDelMotorSePintaAmigable(t *testing.T) {
	useRollbacker(t, &fakeRollbacker{plan: rollbackPlan, runErr: &application.EngineError{
		Kind: application.EngineEnvironmentBusy, Environment: "prod", AttemptID: "01a113d0-7999-7000-8000-000004f91b55"}},
		&fakeConfirmer{answer: true})

	_, stderr, err := run(t, rollbackCmd, "f974d0dee")

	require.Error(t, err)
	assert.Contains(t, stderr, "El ambiente \"prod\" ya tiene un intento en curso")
}

func TestRollback_ExigeElDespliegue(t *testing.T) {
	for _, args := range [][]string{nil, {"a", "b"}} {
		err := rollbackCmd.Args(rollbackCmd, args)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "vex rollback <despliegue>")
	}
	assert.NoError(t, rollbackCmd.Args(rollbackCmd, []string{"91f4b55"}))
	assert.Equal(t, groupRelease, groupOf["rollback"])
	assert.NotNil(t, rollbackCmd.Flags().ShorthandLookup("y"))
}
