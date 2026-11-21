package application

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jairoprogramador/vex/internal/infrastructure/portalclient"
)

// M6 de la revisión de seguridad: el portal no cambia qué código se ejecuta sin que alguien lo acepte.
func TestApplyAuthoritative_UnCambioDePipelineODeImagenPideConfirmacion(t *testing.T) {
	drift := func(change func(*portalclient.AuthoritativePipeline)) *portalclient.AuthoritativeData {
		auth := &portalclient.AuthoritativeData{
			Project:  portalclient.AuthoritativeProject{RepoURL: "https://github.com/local/repo", RepoRef: "main"},
			Pipeline: portalclient.AuthoritativePipeline{URL: "https://github.com/local/pipe", Ref: "main", RuntimeImage: "local/runtime", RuntimeTag: "v1"},
		}
		change(&auth.Pipeline)
		return auth
	}
	sensitive := map[string]struct {
		auth  *portalclient.AuthoritativeData
		field string
	}{
		"la URL del pipeline": {drift(func(p *portalclient.AuthoritativePipeline) { p.URL = "https://github.com/atacante/pipe" }), "pipeline.url"},
		"el ref del pipeline": {drift(func(p *portalclient.AuthoritativePipeline) { p.Ref = "0123456789abcdef0123456789abcdef01234567" }), "pipeline.ref"},
		"la imagen":           {drift(func(p *portalclient.AuthoritativePipeline) { p.RuntimeImage = "ghcr.io/atacante/runtime" }), "runtime.image"},
		"la etiqueta":         {drift(func(p *portalclient.AuthoritativePipeline) { p.RuntimeTag = "v9" }), "runtime.image"},
	}
	for name, tt := range sensitive {
		t.Run(name+": sin quien confirme no se escribe", func(t *testing.T) {
			repo := &stubProjectRepo{}
			project := newSeedProject(t)

			changed, err := ApplyAuthoritative(repo, project, tt.auth, nil)

			var declined *PortalChangeError
			require.ErrorAs(t, err, &declined)
			require.ErrorIs(t, err, ErrPortalChangeNotAccepted)
			assert.False(t, changed)
			assert.Nil(t, repo.saved, "vexconfig.yaml no se toca")
			assert.Equal(t, "https://github.com/local/pipe", project.Pipeline().URL(), "ni el proyecto en memoria: no se clona otra cosa")
			assert.Equal(t, "local/runtime", project.Runtime().Image().Image())
			require.Len(t, declined.Changes, 1)
			assert.Equal(t, tt.field, declined.Changes[0].Field)
			assert.Contains(t, err.Error(), declined.Changes[0].Current+" → "+declined.Changes[0].Proposed, "el mensaje enseña la diferencia")
		})
		t.Run(name+": si se rechaza no se escribe", func(t *testing.T) {
			repo := &stubProjectRepo{}
			var asked []ProjectChange

			_, err := ApplyAuthoritative(repo, newSeedProject(t), tt.auth, func(changes []ProjectChange) (bool, error) {
				asked = changes
				return false, nil
			})

			require.ErrorIs(t, err, ErrPortalChangeNotAccepted)
			assert.Len(t, asked, 1)
			assert.Nil(t, repo.saved)
		})
		t.Run(name+": aceptado se adopta", func(t *testing.T) {
			repo := &stubProjectRepo{}

			changed, err := ApplyAuthoritative(repo, newSeedProject(t), tt.auth, func([]ProjectChange) (bool, error) { return true, nil })

			require.NoError(t, err)
			assert.True(t, changed)
			assert.NotNil(t, repo.saved)
		})
	}
	t.Run("los demás campos se adoptan sin preguntar", func(t *testing.T) {
		repo := &stubProjectRepo{}
		auth := drift(func(*portalclient.AuthoritativePipeline) {})
		auth.Project.RepoURL = "https://github.com/portal/repo"

		changed, err := ApplyAuthoritative(repo, newSeedProject(t), auth, func([]ProjectChange) (bool, error) {
			t.Fatal("no debe preguntar")
			return false, nil
		})

		require.NoError(t, err)
		assert.True(t, changed)
	})
	t.Run("lo que dice el portal se muestra solo con ASCII", func(t *testing.T) {
		auth := drift(func(p *portalclient.AuthoritativePipeline) { p.URL = "https://github.com/lo\u0441al/pipe\x1b[2K" })

		_, err := ApplyAuthoritative(&stubProjectRepo{}, newSeedProject(t), auth, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), `lo\u{441}al/pipe\u{1b}[2K`)
	})
}
