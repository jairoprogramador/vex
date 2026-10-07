package application

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeReleaseEngine struct {
	release Release
	err     error

	calls   []string
	request ReleaseRequest
}

func (f *fakeReleaseEngine) Release(_ context.Context, _ ContainerSpec, req ReleaseRequest) (Release, error) {
	f.calls = append(f.calls, "Release")
	f.request = req
	return f.release, f.err
}

func (f *fakeReleaseEngine) Protect(_ context.Context, _ ContainerSpec, environment string) error {
	f.calls = append(f.calls, "Protect:"+environment)
	return f.err
}

func (f *fakeReleaseEngine) Unprotect(_ context.Context, _ ContainerSpec, environment string) error {
	f.calls = append(f.calls, "Unprotect:"+environment)
	return f.err
}

type releaseHarness struct {
	service *ReleaseService
	engine  *fakeReleaseEngine
	recents *RecentsService
}

func newReleaseHarness(t *testing.T) *releaseHarness {
	t.Helper()
	base := t.TempDir()
	workspace := NewWorkspace(
		&stubProjectRepo{loaded: newProject(t), existsBool: true}, &fakeSources{}, &fakeImages{}, &fakePresenter{},
		LocalExecutorConfig{
			WorkDir: "/work", StoreDir: filepath.Join(base, "store"), SpaceDir: filepath.Join(base, "space"),
			MaterialDir: filepath.Join(base, "material"),
		})
	engine := &fakeReleaseEngine{}
	recents := NewRecentsService(newMemoryStore())
	return &releaseHarness{service: NewReleaseService(workspace, engine, recents), engine: engine, recents: recents}
}

func (h *releaseHarness) knowEnvironments(t *testing.T, values ...string) {
	t.Helper()
	environments := make([]Environment, len(values))
	for i, v := range values {
		environments[i] = Environment{Name: v, Value: v}
	}
	require.NoError(t, h.recents.RememberEnvironments(projectKey, environments))
}

func TestRelease_ResuelveElIdCortoYLanzaConSuNombre(t *testing.T) {
	h := newReleaseHarness(t)
	require.NoError(t, h.recents.RememberDeployments(projectKey, []Deployment{{ID: idB, Environment: "prod"}}))
	h.engine.release = Release{ID: "r1", Environment: "prod", DeploymentID: idB, Version: 5, Name: "v2.1"}

	release, err := h.service.Release(context.Background(), "prod", "f974d0dee", "v2.1")

	require.NoError(t, err)
	assert.Equal(t, 5, release.Version)
	assert.Equal(t, ReleaseRequest{Environment: "prod", DeploymentID: idB, Name: "v2.1"}, h.engine.request)
}

func TestRelease_NoLanzaUnDespliegueDeOtroAmbiente(t *testing.T) {
	h := newReleaseHarness(t)
	require.NoError(t, h.recents.RememberDeployments(projectKey, []Deployment{{ID: idB, Environment: "sand"}}))

	_, err := h.service.Release(context.Background(), "prod", idB, "")

	var wrong *WrongEnvironmentError
	require.ErrorAs(t, err, &wrong)
	assert.Equal(t, "sand", wrong.Actual)
	assert.Equal(t, "prod", wrong.Requested)
	assert.Empty(t, h.engine.calls, "el motor no valida esto: no se le puede dejar publicar lo equivocado")
}

func TestRelease_UnAmbienteMalTecleadoNoLlegaAlMotor(t *testing.T) {
	h := newReleaseHarness(t)
	h.knowEnvironments(t, "sand", "prod")
	require.NoError(t, h.recents.RememberDeployments(projectKey, []Deployment{{ID: idB, Environment: "prod"}}))

	_, err := h.service.Release(context.Background(), "porod", idB, "")

	var unknown *UnknownEnvironmentError
	require.ErrorAs(t, err, &unknown)
	assert.Equal(t, "prod", unknown.Suggestion)
	assert.Empty(t, h.engine.calls)
}

func TestRelease_UnIdDesconocidoSeRechazaSinMotor(t *testing.T) {
	h := newReleaseHarness(t)

	_, err := h.service.Release(context.Background(), "prod", "zzzzzzz", "")

	require.ErrorIs(t, err, ErrUnknownID)
	assert.Empty(t, h.engine.calls)
}

func TestRelease_PropagaElErrorDelMotor(t *testing.T) {
	h := newReleaseHarness(t)
	require.NoError(t, h.recents.RememberDeployments(projectKey, []Deployment{{ID: idB, Environment: "prod"}}))
	boom := errors.New("boom")
	h.engine.err = boom

	_, err := h.service.Release(context.Background(), "prod", idB, "")

	require.ErrorIs(t, err, boom)
}

func TestProtectUnprotect_ValidanElAmbienteAntesDeEscribir(t *testing.T) {
	h := newReleaseHarness(t)
	h.knowEnvironments(t, "sand", "prod")

	require.NoError(t, h.service.Protect(context.Background(), "prod"))
	require.NoError(t, h.service.Unprotect(context.Background(), "prod"))
	assert.Equal(t, []string{"Protect:prod", "Unprotect:prod"}, h.engine.calls)

	h.engine.calls = nil
	var unknown *UnknownEnvironmentError
	require.ErrorAs(t, h.service.Protect(context.Background(), "porod"), &unknown)
	require.ErrorAs(t, h.service.Unprotect(context.Background(), "porod"), &unknown)
	assert.Empty(t, h.engine.calls, "el motor reservaría un ambiente que no existe")
}

func TestProtect_SinCatalogoRecordadoNoOpina(t *testing.T) {
	h := newReleaseHarness(t)

	require.NoError(t, h.service.Protect(context.Background(), "prod"))
	assert.Equal(t, []string{"Protect:prod"}, h.engine.calls)
}
