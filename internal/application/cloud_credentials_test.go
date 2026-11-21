package application

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	proVos "github.com/jairoprogramador/vex/internal/domain/project/vos"
)

// fakeCredentials es una fuente de credenciales que entrega siempre la misma sesión y anota cómo se usó.
type fakeCredentials struct {
	session *fakeSession
	// stepAware, si se da, es la sesión que se entrega: una que distingue pasos.
	stepAware CloudSession
	openErr   error
	requests  []CloudSessionRequest
}

func (f *fakeCredentials) Open(_ context.Context, req CloudSessionRequest) (CloudSession, error) {
	f.requests = append(f.requests, req)
	if f.openErr != nil {
		return nil, f.openErr
	}
	if f.stepAware != nil {
		return f.stepAware, nil
	}
	return f.session, nil
}

type fakeSession struct {
	env      map[string]string
	mounts   []Mount
	closeErr error
	closes   int
}

func (f *fakeSession) Env() map[string]string { return f.env }
func (f *fakeSession) Mounts() []Mount        { return f.mounts }
func (f *fakeSession) Close() error {
	f.closes++
	return f.closeErr
}

func newFakeCredentials() *fakeCredentials {
	return &fakeCredentials{session: &fakeSession{
		env:    map[string]string{"VEX_CLOUD_CONTRACT": "nube/v1", "ARM_CLIENT_SECRET": "de-la-sesion"},
		mounts: []Mount{{Source: "/host/credenciales/abc", Target: "/vex/credenciales", ReadOnly: true}},
	}}
}

func TestEnvCredentials_AbreUnaSesionVacia(t *testing.T) {
	session, err := envCredentials{}.Open(context.Background(), CloudSessionRequest{})

	require.NoError(t, err)
	assert.Empty(t, session.Env())
	assert.Empty(t, session.Mounts())
	assert.NoError(t, session.Close())
}

func TestSelectCredentials(t *testing.T) {
	portal := newFakeCredentials()

	t.Run("sin bloque cloud usa env aunque haya fuente del portal", func(t *testing.T) {
		got, err := selectCredentials(newProject(t), portal)
		require.NoError(t, err)
		assert.Equal(t, envCredentials{}, got)
	})
	t.Run("credentials env usa env", func(t *testing.T) {
		got, err := selectCredentials(newProject(t, withCloud(t, "azure", proVos.CredentialsEnv)), portal)
		require.NoError(t, err)
		assert.Equal(t, envCredentials{}, got)
	})
	t.Run("portal usa la fuente inyectada", func(t *testing.T) {
		got, err := selectCredentials(newProject(t, withCloud(t, "", proVos.CredentialsPortal)), portal)
		require.NoError(t, err)
		assert.Same(t, portal, got)
	})
	t.Run("portal sin fuente es un error", func(t *testing.T) {
		_, err := selectCredentials(newProject(t, withCloud(t, "", proVos.CredentialsPortal)), nil)
		assert.ErrorIs(t, err, ErrPortalCredentialsUnavailable)
	})
}

func TestMergeSecrets(t *testing.T) {
	base := map[string]string{"A": "base", "B": "base"}
	extra := map[string]string{"B": "extra", "C": "extra"}

	merged := mergeSecrets(base, extra)

	assert.Equal(t, map[string]string{"A": "base", "B": "extra", "C": "extra"}, merged, "gana extra")
	assert.Equal(t, map[string]string{"A": "base", "B": "base"}, base, "no modifica base")
	assert.Equal(t, map[string]string{"B": "extra", "C": "extra"}, extra, "no modifica extra")
	assert.Nil(t, mergeSecrets(nil, nil), "sin nada que combinar no inventa un mapa")
	assert.Equal(t, extra, mergeSecrets(nil, extra))
}

func TestCloseCloudSession_UnFalloSoloSeAvisa(t *testing.T) {
	presenter := &fakePresenter{}

	closeCloudSession(&fakeSession{closeErr: errors.New("disco lleno")}, presenter)

	require.Len(t, presenter.warnings, 1)
	assert.Contains(t, presenter.warnings[0], "disco lleno")
}
