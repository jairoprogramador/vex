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
)

type fakeQueries struct {
	calls []string
	err   error

	attempts    application.AttemptList
	detail      application.AttemptDetail
	logs        application.AttemptLogs
	deployments application.DeploymentList
	releases    application.ReleaseList
	lastLimit   int
	lastFailed  bool
}

func (f *fakeQueries) ListAttempts(_ context.Context, environment string, limit int) (application.AttemptList, error) {
	f.calls, f.lastLimit = append(f.calls, "ListAttempts:"+environment), limit
	return f.attempts, f.err
}

func (f *fakeQueries) ShowAttempt(_ context.Context, ref string) (application.AttemptDetail, error) {
	f.calls = append(f.calls, "ShowAttempt:"+ref)
	return f.detail, f.err
}

func (f *fakeQueries) Logs(_ context.Context, ref string, onlyFailed bool) (application.AttemptLogs, error) {
	f.calls, f.lastFailed = append(f.calls, "Logs:"+ref), onlyFailed
	return f.logs, f.err
}

func (f *fakeQueries) ListDeployments(_ context.Context, environment string, limit int) (application.DeploymentList, error) {
	f.calls, f.lastLimit = append(f.calls, "ListDeployments:"+environment), limit
	f.deployments.Environment = environment
	return f.deployments, f.err
}

func (f *fakeQueries) ListReleases(_ context.Context, environment string, limit int) (application.ReleaseList, error) {
	f.calls, f.lastLimit = append(f.calls, "ListReleases:"+environment), limit
	f.releases.Environment = environment
	return f.releases, f.err
}

// runCommand ejecuta el RunE de un subcomando con un servicio falso y devuelve lo que pintó y lo que dijo por stderr.
func runCommand(t *testing.T, c *cobra.Command, fake *fakeQueries, mode config.ExecutionMode, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	previousQueries, previousMode := newQueries, queryMode
	built := false
	newQueries = func() (queries, error) { built = true; return fake, nil }
	queryMode = func() (config.ExecutionMode, error) { return mode, nil }
	t.Cleanup(func() { newQueries, queryMode = previousQueries, previousMode })

	var out, errOut bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&errOut)
	c.SetContext(context.Background())
	c.SilenceErrors = false
	if err := c.Args(c, args); err != nil {
		return "", "", err
	}
	err = c.RunE(c, args)
	if mode != config.ModeLocal {
		assert.False(t, built, "en modo remoto ni se construye el servicio")
	}
	return out.String(), errOut.String(), err
}

func resetListFlags(t *testing.T, c *cobra.Command) {
	t.Helper()
	require.NoError(t, c.Flags().Set("limit", "10"))
	require.NoError(t, c.Flags().Set("all", "false"))
}

func TestLs_PintaLaListaYPideLosDiezUltimos(t *testing.T) {
	resetListFlags(t, lsCmd)
	fake := &fakeQueries{attempts: application.AttemptList{Environment: "sand", Total: 1, Attempts: []application.AttemptSummary{
		{ID: "01a113d0-74b0-7966-80ad-a84c35ab306c", Environment: "sand", UntilStep: "deploy", Requester: "ana",
			Status: application.AttemptSucceeded, StartedAt: time.Now()},
	}}}

	stdout, _, err := runCommand(t, lsCmd, fake, config.ModeLocal, "sand")

	require.NoError(t, err)
	assert.Equal(t, []string{"ListAttempts:sand"}, fake.calls)
	assert.Equal(t, 10, fake.lastLimit)
	assert.Contains(t, stdout, "sand · 1 intento")
	assert.Contains(t, stdout, "5ab306c")
}

func TestLs_LimiteYTodos(t *testing.T) {
	fake := &fakeQueries{}

	resetListFlags(t, lsCmd)
	require.NoError(t, lsCmd.Flags().Set("limit", "3"))
	_, _, err := runCommand(t, lsCmd, fake, config.ModeLocal, "sand")
	require.NoError(t, err)
	assert.Equal(t, 3, fake.lastLimit)

	resetListFlags(t, lsCmd)
	require.NoError(t, lsCmd.Flags().Set("all", "true"))
	_, _, err = runCommand(t, lsCmd, fake, config.ModeLocal, "sand")
	require.NoError(t, err)
	assert.Equal(t, 0, fake.lastLimit, "--all pide sin límite")
	resetListFlags(t, lsCmd)
}

func TestComandosConAmbiente_ExigenElAmbienteYLoDicenConElUso(t *testing.T) {
	for _, c := range []*cobra.Command{lsCmd, deploymentsCmd, releasesCmd} {
		t.Run(c.Name(), func(t *testing.T) {
			err := c.Args(c, nil)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "indica el ambiente: vex "+c.Name()+" <ambiente>")
			assert.NoError(t, c.Args(c, []string{"sand"}))
			assert.Error(t, c.Args(c, []string{"sand", "extra"}))
		})
	}
}

func TestShow_SinArgumentosUsaElUltimoYConArgumentoElIdPedido(t *testing.T) {
	fake := &fakeQueries{}

	_, _, err := runCommand(t, showCmd, fake, config.ModeLocal)
	require.NoError(t, err)
	_, _, err = runCommand(t, showCmd, fake, config.ModeLocal, "5ab306c")
	require.NoError(t, err)

	assert.Equal(t, []string{"ShowAttempt:", "ShowAttempt:5ab306c"}, fake.calls)
}

func TestLog_FailedFiltraLosFallidos(t *testing.T) {
	fake := &fakeQueries{}
	t.Cleanup(func() { require.NoError(t, logCmd.Flags().Set("failed", "false")) })

	_, _, err := runCommand(t, logCmd, fake, config.ModeLocal)
	require.NoError(t, err)
	assert.False(t, fake.lastFailed)

	require.NoError(t, logCmd.Flags().Set("failed", "true"))
	stdout, _, err := runCommand(t, logCmd, fake, config.ModeLocal, "5ab306c")
	require.NoError(t, err)
	assert.True(t, fake.lastFailed)
	assert.Contains(t, stdout, "salida de los comandos fallidos")
}

func TestDeploymentsYReleases(t *testing.T) {
	resetListFlags(t, deploymentsCmd)
	resetListFlags(t, releasesCmd)
	fake := &fakeQueries{}

	out1, _, err := runCommand(t, deploymentsCmd, fake, config.ModeLocal, "prod")
	require.NoError(t, err)
	out2, _, err := runCommand(t, releasesCmd, fake, config.ModeLocal, "prod")
	require.NoError(t, err)

	assert.Equal(t, []string{"ListDeployments:prod", "ListReleases:prod"}, fake.calls)
	assert.Contains(t, out1, "prod: todavía no hay despliegues")
	assert.Contains(t, out2, "prod: todavía no se ha lanzado nada")
}

func TestUnaConsultaEnModoRemotoExplicaQueHacer(t *testing.T) {
	fake := &fakeQueries{}

	_, _, err := runCommand(t, lsCmd, fake, config.ModeRemote, "sand")

	require.ErrorIs(t, err, errQueryNeedsLocalMode)
	assert.Contains(t, err.Error(), "vex config mode=local")
	assert.Empty(t, fake.calls)
}

func TestUnErrorDelServicioSePintaAmigableYNoSeDuplica(t *testing.T) {
	fake := &fakeQueries{err: &application.UnknownEnvironmentError{
		Name: "sandd", Known: []string{"sand", "prod"}, Suggestion: "sand"}}

	stdout, stderr, err := runCommand(t, lsCmd, fake, config.ModeLocal, "sandd")

	require.Error(t, err)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "Error: El ambiente «sandd» no existe en este pipeline.")
	assert.Contains(t, stderr, "¿Quisiste decir «sand»?")
	assert.True(t, lsCmd.SilenceErrors, "cobra no vuelve a imprimirlo")
}

func TestUnErrorSinNadaQueAnadirNoImprimeNada(t *testing.T) {
	fake := &fakeQueries{err: application.ErrAttemptFailed}

	_, stderr, err := runCommand(t, showCmd, fake, config.ModeLocal)

	require.True(t, errors.Is(err, application.ErrAttemptFailed))
	assert.Empty(t, stderr)
}

func TestLosComandosDeConsultaEstanEnElGrupoConsultar(t *testing.T) {
	for _, name := range []string{"ls", "show", "log", "deployments", "releases"} {
		assert.Equal(t, groupQuery, groupOf[name], name)
	}
	for _, c := range []*cobra.Command{lsCmd, showCmd, logCmd, deploymentsCmd, releasesCmd} {
		found, _, err := vexCmd.Find([]string{c.Name()})
		require.NoError(t, err)
		assert.Same(t, c, found, "%s está registrado en la raíz", c.Name())
	}
}
