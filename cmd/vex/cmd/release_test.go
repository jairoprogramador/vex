package cmd

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jairoprogramador/vex/internal/application"
)

type fakeReleaser struct {
	release application.Release
	err     error

	calls []string
}

func (f *fakeReleaser) Release(_ context.Context, environment, ref, name string) (application.Release, error) {
	f.calls = append(f.calls, "release "+environment+" "+ref+" "+name)
	return f.release, f.err
}

func (f *fakeReleaser) Protect(_ context.Context, environment string) error {
	f.calls = append(f.calls, "protect "+environment)
	return f.err
}

func (f *fakeReleaser) Unprotect(_ context.Context, environment string) error {
	f.calls = append(f.calls, "unprotect "+environment)
	return f.err
}

func useReleaser(t *testing.T, fake *fakeReleaser) {
	t.Helper()
	before := newReleaser
	newReleaser = func() (releaser, error) { return fake, nil }
	t.Cleanup(func() { newReleaser, releaseName = before, "" })
}

func TestRelease_PasaAmbienteDespliegueYNombre(t *testing.T) {
	fake := &fakeReleaser{release: application.Release{
		Environment: "prod", DeploymentID: "01a113d0-7845-79ee-9381-be4f974d0dee", Version: 5, Name: "v2.1"}}
	useReleaser(t, fake)
	require.NoError(t, releaseCmd.Flags().Set("name", "v2.1"))

	stdout, _, err := run(t, releaseCmd, "prod", "f974d0dee")

	require.NoError(t, err)
	assert.Equal(t, []string{"release prod f974d0dee v2.1"}, fake.calls)
	assert.Contains(t, stdout, "✔ prod: lanzado el despliegue 74d0dee como «v2.1» (versión 5).")
}

func TestRelease_ErrorSeTraduceYSaleConUno(t *testing.T) {
	useReleaser(t, &fakeReleaser{err: &application.WrongEnvironmentError{DeploymentID: "01a113d0-7845-79ee-9381-be4f974d0dee", Actual: "sand", Requested: "prod"}})

	_, stderr, err := run(t, releaseCmd, "prod", "f974d0dee")

	require.Error(t, err)
	assert.Equal(t, 1, exitCode(err))
	assert.Contains(t, stderr, "es de sand, no de prod")
}

func TestProtectYUnprotect_AvisanDeLoQueHacen(t *testing.T) {
	fake := &fakeReleaser{}
	useReleaser(t, fake)

	protectOut, _, err := run(t, protectCmd, "prod")
	require.NoError(t, err)
	unprotectOut, _, err := run(t, unprotectCmd, "prod")
	require.NoError(t, err)

	assert.Equal(t, []string{"protect prod", "unprotect prod"}, fake.calls)
	assert.Contains(t, protectOut, "NO impide desplegar ni hacer rollback")
	assert.Contains(t, unprotectOut, "prod ya no está protegido")
}

func TestReleaseProtectUnprotect_ExigenSusArgumentos(t *testing.T) {
	assert.EqualError(t, releaseCmd.Args(releaseCmd, []string{"prod"}), "indica el ambiente y el despliegue: vex release <ambiente> <despliegue>")
	assert.NoError(t, releaseCmd.Args(releaseCmd, []string{"prod", "91f4b55"}))
	assert.Error(t, protectCmd.Args(protectCmd, nil))
	assert.Error(t, unprotectCmd.Args(unprotectCmd, []string{"a", "b"}))
	for _, name := range []string{"release", "protect", "unprotect"} {
		assert.Equal(t, groupRelease, groupOf[name], name)
	}
}
