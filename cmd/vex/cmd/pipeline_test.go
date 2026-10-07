package cmd

import (
	"bytes"
	"context"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jairoprogramador/vex/internal/application"
	"github.com/jairoprogramador/vex/internal/config"
)

type fakePipeline struct {
	environments []application.Environment
	steps        []application.PipelineStep
	checkErr     error
	err          error
	checked      []string
}

func (f *fakePipeline) Environments(context.Context) ([]application.Environment, error) {
	return f.environments, f.err
}

func (f *fakePipeline) Steps(context.Context) ([]application.PipelineStep, error) {
	return f.steps, f.err
}

func (f *fakePipeline) Check(_ context.Context, step, environment string) error {
	f.checked = append(f.checked, step+":"+environment)
	return f.checkErr
}

func runPipelineCommand(t *testing.T, c *cobra.Command, fake *fakePipeline, mode config.ExecutionMode, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	previousBuild, previousMode := newPipelineInspector, queryMode
	newPipelineInspector = func() (pipelineInspector, error) { return fake, nil }
	queryMode = func() (config.ExecutionMode, error) { return mode, nil }
	t.Cleanup(func() { newPipelineInspector, queryMode = previousBuild, previousMode })

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

func TestEnvs_ListaLosAmbientesConSuProteccion(t *testing.T) {
	fake := &fakePipeline{environments: []application.Environment{
		{Value: "sand", Name: "sandbox"}, {Value: "prod", Name: "production", Protected: true},
	}}

	stdout, _, err := runPipelineCommand(t, envsCmd, fake, config.ModeLocal)

	require.NoError(t, err)
	assert.Contains(t, stdout, "sand")
	assert.Contains(t, stdout, "manual (protegido)")
}

func TestSteps_AvisaDeLosPasosQueChocanConUnComando(t *testing.T) {
	fake := &fakePipeline{steps: []application.PipelineStep{{Name: "test", Order: 1}, {Name: "log", Order: 2}}}

	stdout, _, err := runPipelineCommand(t, stepsCmd, fake, config.ModeLocal)

	require.NoError(t, err)
	assert.Contains(t, stdout, "El paso «log» se llama igual que un comando de vex")
	assert.Contains(t, stdout, "vex run log <ambiente>")
	assert.NotContains(t, stdout, "«test» se llama")
}

func TestCheck_TodoEnOrden(t *testing.T) {
	fake := &fakePipeline{}

	stdout, stderr, err := runPipelineCommand(t, checkCmd, fake, config.ModeLocal, "deploy", "sand")

	require.NoError(t, err)
	assert.Equal(t, []string{"deploy:sand"}, fake.checked)
	assert.Contains(t, stdout, "Todo en orden")
	assert.Contains(t, stdout, "No se ejecutó nada")
	assert.Empty(t, stderr)
}

func TestCheck_UnPasoConElNombreDeUnComandoAvisa(t *testing.T) {
	fake := &fakePipeline{}

	_, stderr, err := runPipelineCommand(t, checkCmd, fake, config.ModeLocal, "log", "sand")

	require.NoError(t, err)
	assert.Contains(t, stderr, "vex run log <ambiente>")
}

func TestCheck_SiFallaDiceQueFaltaYTerminaConError(t *testing.T) {
	fake := &fakePipeline{checkErr: &application.CheckFailedError{
		Environment: "sand", UntilStep: "deploy",
		Missing: []application.MissingVariables{{Step: "deploy", Variables: []string{"db_url"}}},
	}}

	stdout, stderr, err := runPipelineCommand(t, checkCmd, fake, config.ModeLocal, "deploy", "sand")

	require.Error(t, err, "el código de salida es 1")
	assert.Empty(t, stdout, "no se anuncia éxito")
	assert.Contains(t, stderr, "Faltan variables en el paso «deploy»: db_url")
	assert.Equal(t, 1, exitCode(err))
}

func TestCheck_UnAmbienteMalTecleadoSugiere(t *testing.T) {
	fake := &fakePipeline{checkErr: &application.UnknownEnvironmentError{
		Name: "sandd", Known: []string{"sand", "prod"}, Suggestion: "sand"}}

	_, stderr, err := runPipelineCommand(t, checkCmd, fake, config.ModeLocal, "deploy", "sandd")

	require.Error(t, err)
	assert.Contains(t, stderr, "¿Quisiste decir «sand»?")
}

func TestCheck_ExigeElPasoYElAmbiente(t *testing.T) {
	for _, args := range [][]string{nil, {"deploy"}, {"a", "b", "c"}} {
		err := checkCmd.Args(checkCmd, args)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "vex check <paso> <ambiente>")
	}
	assert.NoError(t, checkCmd.Args(checkCmd, []string{"deploy", "sand"}))
}

func TestComandosDelPipeline_EnModoRemotoExplicanQueHacer(t *testing.T) {
	for _, c := range []*cobra.Command{envsCmd, stepsCmd} {
		_, _, err := runPipelineCommand(t, c, &fakePipeline{}, config.ModeRemote)

		assert.ErrorIs(t, err, errQueryNeedsLocalMode, c.Name())
	}
}

func TestNoCheck_EstaEnLasDosFormasDeEjecutar(t *testing.T) {
	assert.NotNil(t, vexCmd.Flags().Lookup("no-check"), "vex <paso> <ambiente> --no-check")
	assert.NotNil(t, runCmd.Flags().Lookup("no-check"), "vex run <paso> <ambiente> --no-check")
	assert.False(t, noCheck, "el pre-vuelo está activo por defecto")
}

func TestReservedCommandNames_IncluyeLosComandosYSusAlias(t *testing.T) {
	names := reservedCommandNames()

	for _, name := range []string{"run", "check", "ls", "list", "log", "logs", "envs", "steps", "deployments", "deploys"} {
		assert.True(t, names[name], name)
	}
	assert.False(t, names["test"], "un paso corriente no está reservado")
}

func TestLosComandosDelPipelineEstanEnSusGrupos(t *testing.T) {
	assert.Equal(t, groupRun, groupOf["check"])
	assert.Equal(t, groupQuery, groupOf["envs"])
	assert.Equal(t, groupQuery, groupOf["steps"])
	for _, c := range []*cobra.Command{envsCmd, stepsCmd, checkCmd} {
		found, _, err := vexCmd.Find([]string{c.Name()})
		require.NoError(t, err)
		assert.Same(t, c, found)
	}
}

type recordingRunner struct{ calls []string }

func (r *recordingRunner) Run(_ context.Context, step, environment string) error {
	r.calls = append(r.calls, step+":"+environment)
	return nil
}

// buildWith ejecuta runStep con un constructor falso y devuelve con qué parámetros se construyó el ejecutor.
func buildWith(t *testing.T, c *cobra.Command, args []string, noCheckFlag bool) (gotPreflight bool, runner *recordingRunner) {
	t.Helper()
	previousBuild, previousMode, previousNoCheck := newRunner, modeFlag, noCheck
	runner = &recordingRunner{}
	newRunner = func(_ config.ExecutionMode, _, preflight bool) (application.Runner, error) {
		gotPreflight = preflight
		return runner, nil
	}
	modeFlag, noCheck = string(config.ModeLocal), noCheckFlag
	t.Cleanup(func() { newRunner, modeFlag, noCheck = previousBuild, previousMode, previousNoCheck })

	c.SetContext(context.Background())
	require.NoError(t, runStep(c, args))
	return gotPreflight, runner
}

func TestRunStep_ElPreVueloEstaActivoSalvoConNoCheck(t *testing.T) {
	preflight, runner := buildWith(t, vexCmd, []string{"test", "sand"}, false)
	assert.True(t, preflight, "por defecto se comprueba antes de ejecutar")
	assert.Equal(t, []string{"test:sand"}, runner.calls)

	preflight, _ = buildWith(t, vexCmd, []string{"test", "sand"}, true)
	assert.False(t, preflight, "--no-check va directo al intento")
}

func TestRunStep_VexRunSeComportaIgualQueElAtajo(t *testing.T) {
	preflight, runner := buildWith(t, runCmd, []string{"release", "prod"}, false)

	assert.True(t, preflight)
	assert.Equal(t, []string{"release:prod"}, runner.calls, "vex run permite un paso que se llame como un comando")

	preflight, _ = buildWith(t, runCmd, []string{"release", "prod"}, true)
	assert.False(t, preflight)
}
