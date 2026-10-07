package cmd

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jairoprogramador/vex/internal/application"
	"github.com/jairoprogramador/vex/internal/config"
	"github.com/jairoprogramador/vex/internal/infrastructure/console"
)

type fakeDiagnoser struct {
	report application.WhyReport
	err    error

	gotRef, gotAgainst string
}

func (f *fakeDiagnoser) Why(_ context.Context, ref, against string) (application.WhyReport, error) {
	f.gotRef, f.gotAgainst = ref, against
	return f.report, f.err
}

type fakeAbandoner struct {
	plan       application.AbandonPlan
	planErr    error
	abandonErr error

	planRef   string
	abandoned bool
}

func (f *fakeAbandoner) Plan(_ context.Context, ref string) (application.AbandonPlan, error) {
	f.planRef = ref
	return f.plan, f.planErr
}

func (f *fakeAbandoner) Abandon(context.Context, application.AbandonPlan) error {
	f.abandoned = true
	return f.abandonErr
}

type fakeConfirmer struct {
	answer  bool
	err     error
	asked   int
	lastAsk string
}

func (f *fakeConfirmer) Confirm(question string) (bool, error) {
	f.asked++
	f.lastAsk = question
	return f.answer, f.err
}

func run(t *testing.T, c *cobra.Command, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	queryModeBefore := queryMode
	queryMode = func() (config.ExecutionMode, error) { return config.ModeLocal, nil }
	t.Cleanup(func() { queryMode = queryModeBefore })

	var out, errOut bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&errOut)
	c.SetContext(context.Background())
	if err := c.Args(c, args); err != nil {
		return "", "", err
	}
	err = c.RunE(c, args)
	return out.String(), errOut.String(), err
}

func useDiagnoser(t *testing.T, fake *fakeDiagnoser) {
	t.Helper()
	before := newDiagnoser
	newDiagnoser = func() (diagnoser, error) { return fake, nil }
	t.Cleanup(func() { newDiagnoser = before; vsFlag = "" })
}

func useAbandoner(t *testing.T, fake *fakeAbandoner, confirm *fakeConfirmer) {
	t.Helper()
	beforeBuild, beforeConfirm := newAbandoner, newConfirmer
	newAbandoner = func() (abandoner, error) { return fake, nil }
	newConfirmer = func() confirmer { return confirm }
	t.Cleanup(func() { newAbandoner, newConfirmer, yesFlag = beforeBuild, beforeConfirm, false })
}

var abandonPlan = application.AbandonPlan{Attempt: application.AttemptDetail{AttemptSummary: application.AttemptSummary{
	ID: "01a113d0-7999-7000-8000-000004f91b55", Environment: "sand", StartedAt: time.Now().Add(-2 * time.Hour),
}}}

func TestWhy_PasaElIdYLaReferenciaYPintaElDiagnostico(t *testing.T) {
	fake := &fakeDiagnoser{report: application.WhyReport{AttemptID: "01a113d0-7845-79ee-9381-be4f9efac5ab", Outcome: application.WhyNotAFailure}}
	useDiagnoser(t, fake)
	require.NoError(t, whyCmd.Flags().Set("vs", "91f4b55"))

	stdout, _, err := run(t, whyCmd, "efac5ab")

	require.NoError(t, err)
	assert.Equal(t, "efac5ab", fake.gotRef)
	assert.Equal(t, "91f4b55", fake.gotAgainst)
	assert.Contains(t, stdout, "terminó bien")
}

func TestWhy_SinArgumentosPideElUltimoFallido(t *testing.T) {
	fake := &fakeDiagnoser{}
	useDiagnoser(t, fake)

	_, _, err := run(t, whyCmd)

	require.NoError(t, err)
	assert.Empty(t, fake.gotRef, "el servicio elige el último intento fallido")
	assert.Empty(t, fake.gotAgainst)
}

func TestWhy_UnErrorSePintaAmigable(t *testing.T) {
	useDiagnoser(t, &fakeDiagnoser{err: application.ErrNoFailedAttempt})

	stdout, stderr, err := run(t, whyCmd)

	require.Error(t, err)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "No recuerdo ningún intento fallido")
}

func TestAbandon_ConfirmadoAbandona(t *testing.T) {
	fake, confirm := &fakeAbandoner{plan: abandonPlan}, &fakeConfirmer{answer: true}
	useAbandoner(t, fake, confirm)

	stdout, _, err := run(t, abandonCmd, "4f91b55")

	require.NoError(t, err)
	assert.Equal(t, "4f91b55", fake.planRef)
	assert.True(t, fake.abandoned)
	assert.Equal(t, 1, confirm.asked)
	assert.Contains(t, stdout, "NO detiene comandos", "avisa antes de pedir confirmación")
	assert.Contains(t, stdout, "abandonado: el ambiente sand queda libre")
}

func TestAbandon_SiSeRechazaNoSeAbandonaNada(t *testing.T) {
	fake, confirm := &fakeAbandoner{plan: abandonPlan}, &fakeConfirmer{answer: false}
	useAbandoner(t, fake, confirm)

	stdout, stderr, err := run(t, abandonCmd)

	require.NoError(t, err)
	assert.False(t, fake.abandoned)
	assert.Contains(t, stderr, "No se abandonó nada.")
	assert.NotContains(t, stdout, "queda libre")
}

func TestAbandon_ConYNoPregunta(t *testing.T) {
	fake, confirm := &fakeAbandoner{plan: abandonPlan}, &fakeConfirmer{answer: false}
	useAbandoner(t, fake, confirm)
	require.NoError(t, abandonCmd.Flags().Set("yes", "true"))

	_, _, err := run(t, abandonCmd, "4f91b55")

	require.NoError(t, err)
	assert.True(t, fake.abandoned)
	assert.Zero(t, confirm.asked, "-y confirma sin preguntar")
}

func TestAbandon_SinTerminalExigeY(t *testing.T) {
	fake, confirm := &fakeAbandoner{plan: abandonPlan}, &fakeConfirmer{err: console.ErrNotInteractive}
	useAbandoner(t, fake, confirm)

	_, stderr, err := run(t, abandonCmd, "4f91b55")

	require.ErrorIs(t, err, console.ErrNotInteractive)
	assert.False(t, fake.abandoned, "sin poder confirmar no se abandona")
	assert.Contains(t, stderr, "Añade -y")
}

func TestAbandon_UnPlanQueFallaNoPideConfirmacion(t *testing.T) {
	fake := &fakeAbandoner{planErr: &application.AttemptFinishedError{
		AttemptID: "01a113d0-7999-7000-8000-000004f91b55", Status: application.AttemptSucceeded}}
	confirm := &fakeConfirmer{answer: true}
	useAbandoner(t, fake, confirm)

	_, stderr, err := run(t, abandonCmd, "4f91b55")

	require.Error(t, err)
	assert.Zero(t, confirm.asked, "no se pregunta por algo que no se puede hacer")
	assert.Contains(t, stderr, "ya terminó bien")
}

func TestAbandon_SiElMotorFallaAlAbandonarNoSeAnunciaExito(t *testing.T) {
	fake := &fakeAbandoner{plan: abandonPlan, abandonErr: errors.New("boom")}
	useAbandoner(t, fake, &fakeConfirmer{answer: true})

	stdout, _, err := run(t, abandonCmd, "4f91b55")

	require.Error(t, err)
	assert.NotContains(t, stdout, "queda libre")
}

func TestWhyYAbandonEstanRegistradosEnSusGrupos(t *testing.T) {
	assert.Equal(t, groupQuery, groupOf["why"])
	assert.Equal(t, groupRelease, groupOf["abandon"])
	for _, c := range []*cobra.Command{whyCmd, abandonCmd} {
		found, _, err := vexCmd.Find([]string{c.Name()})
		require.NoError(t, err)
		assert.Same(t, c, found)
	}
	assert.NotNil(t, whyCmd.Flags().Lookup("vs"))
	assert.NotNil(t, abandonCmd.Flags().ShorthandLookup("y"))
}
