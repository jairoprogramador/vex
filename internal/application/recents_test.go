package application

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memoryStore struct {
	data    map[string]Recents
	loadErr error
	saveErr error
}

func newMemoryStore() *memoryStore { return &memoryStore{data: map[string]Recents{}} }

func (m *memoryStore) Load(projectID string) (Recents, error) { return m.data[projectID], m.loadErr }
func (m *memoryStore) Save(projectID string, r Recents) error {
	m.data[projectID] = r
	return m.saveErr
}

const (
	idA = "01a113d0-74b0-7966-80ad-a84c3d0dc694" // el más antiguo
	idB = "01a113d0-7845-79ee-9381-be4f974d0dee"
	idC = "01a113d1-0001-7000-8000-aaaaaaabc694" // termina igual que idA en "c694"
)

func newRecents(t *testing.T) (*RecentsService, *memoryStore) {
	t.Helper()
	store := newMemoryStore()
	return NewRecentsService(store), store
}

func TestRecents_LosIntentosSeGuardanDelMasRecienteAlMasAntiguo(t *testing.T) {
	recents, store := newRecents(t)

	require.NoError(t, recents.RememberAttempt("p", RecentAttempt{ID: idA, Environment: "sand"}))
	require.NoError(t, recents.RememberAttempt("p", RecentAttempt{ID: idC, Environment: "sand"}))
	require.NoError(t, recents.RememberAttempt("p", RecentAttempt{ID: idB, Environment: "prod"}))

	ids := []string{}
	for _, a := range store.data["p"].Attempts {
		ids = append(ids, a.ID)
	}
	assert.Equal(t, []string{idC, idB, idA}, ids)
}

func TestRecents_UnIntentoRepetidoSeActualizaSinPerderDatos(t *testing.T) {
	recents, store := newRecents(t)
	require.NoError(t, recents.RememberAttempt("p", RecentAttempt{ID: idA, Environment: "sand", UntilStep: "test"}))

	require.NoError(t, recents.RememberAttempt("p", RecentAttempt{ID: idA, Status: AttemptFailed, Cause: CauseError}))

	require.Len(t, store.data["p"].Attempts, 1)
	assert.Equal(t, RecentAttempt{
		ID: idA, Environment: "sand", UntilStep: "test", Status: AttemptFailed, Cause: CauseError,
	}, store.data["p"].Attempts[0], "el resultado completa lo que ya se sabía, sin borrarlo con vacíos")
}

func TestRecents_SoloSeGuardanLosUltimos100(t *testing.T) {
	recents, store := newRecents(t)
	for i := 0; i < 130; i++ {
		id := fmt.Sprintf("01a113d0-%04x-7000-8000-%012x", i, i)
		require.NoError(t, recents.RememberAttempt("p", RecentAttempt{ID: id}))
	}

	attempts := store.data["p"].Attempts
	assert.Len(t, attempts, maxRecents)
	assert.Equal(t, fmt.Sprintf("01a113d0-%04x-7000-8000-%012x", 129, 129), attempts[0].ID, "se conservan los más recientes")
}

func TestRecents_UltimoIntentoYUltimoFallido(t *testing.T) {
	recents, _ := newRecents(t)
	require.NoError(t, recents.RememberAttempt("p", RecentAttempt{ID: idA, Status: AttemptFailed}))
	require.NoError(t, recents.RememberAttempt("p", RecentAttempt{ID: idB, Status: AttemptSucceeded}))

	last, ok, err := recents.LastAttempt("p", false)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, idB, last.ID)

	failed, ok, err := recents.LastAttempt("p", true)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, idA, failed.ID, "el último FALLIDO salta al exitoso más reciente")

	_, ok, err = recents.LastAttempt("otro-proyecto", false)
	require.NoError(t, err)
	assert.False(t, ok, "cada proyecto tiene su memoria")
}

func TestRecents_ResolverUnId(t *testing.T) {
	recents, _ := newRecents(t)
	for _, id := range []string{idA, idB, idC} {
		require.NoError(t, recents.RememberAttempt("p", RecentAttempt{ID: id}))
	}

	tests := []struct {
		name    string
		ref     string
		want    string
		wantErr error
	}{
		{"el final único", "f974d0dee", idB, nil},
		{"mayúsculas y espacios no importan", "  F974D0DEE ", idB, nil},
		{"el id completo siempre vale, aunque no se haya visto", "01a1ffff-0000-7000-8000-000000000000", "01a1ffff-0000-7000-8000-000000000000", nil},
		{"demasiado corto", "c694", "", ErrIDTooShort},
		{"desconocido", "zzzzzzz", "", ErrUnknownID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := recents.ResolveAttempt("p", tt.ref)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRecents_UnIdAmbiguoDiceLasCoincidencias(t *testing.T) {
	recents, _ := newRecents(t)
	require.NoError(t, recents.RememberAttempt("p", RecentAttempt{ID: idA}))
	require.NoError(t, recents.RememberAttempt("p", RecentAttempt{ID: idC}))

	_, err := recents.ResolveAttempt("p", "aaaabc694")

	assert.NoError(t, err, "con más caracteres ya se distinguen")
	_, err = recents.ResolveAttempt("p", "3d0dc694")
	assert.NoError(t, err)

	same1, same2 := "01a113d2-0001-7000-8000-000000123456", "01a113d3-0002-7000-8000-ffffff123456"
	require.NoError(t, recents.RememberAttempt("p", RecentAttempt{ID: same1}))
	require.NoError(t, recents.RememberAttempt("p", RecentAttempt{ID: same2}))
	_, err = recents.ResolveAttempt("p", "123456")

	var ambiguous *AmbiguousIDError
	require.ErrorAs(t, err, &ambiguous)
	assert.ElementsMatch(t, []string{same1, same2}, ambiguous.Candidates)
}

func TestRecents_ElCatalogoYLosDespliegues(t *testing.T) {
	recents, _ := newRecents(t)
	require.NoError(t, recents.RememberDeployments("p", []Deployment{
		{ID: idA, Environment: "prod"}, {ID: idB, Environment: "prod"}, {ID: idA, Environment: "prod"},
	}))
	require.NoError(t, recents.RememberCatalog("p",
		[]Environment{{Value: "sand", Protected: false}, {Value: "prod", Protected: true}},
		[]PipelineStep{{Name: "test", Order: 1}}))

	last, ok, err := recents.LastDeployment("p")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, idB, last.ID)

	catalog, ok, err := recents.Catalog("p")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Len(t, catalog.Environments, 2)

	env, ok, err := recents.EnvironmentOf("p", idB)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "prod", env)

	deployment, err := recents.ResolveDeployment("p", "be4f974d0dee")
	require.NoError(t, err)
	assert.Equal(t, idB, deployment)

	_, ok, err = recents.Catalog("otro")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestRecents_UnAlmacenQueFallaSePropagaPeroNoPanica(t *testing.T) {
	recents, store := newRecents(t)
	store.loadErr = errors.New("disco roto")

	assert.Error(t, recents.RememberAttempt("p", RecentAttempt{ID: idA}))
	_, _, err := recents.LastAttempt("p", false)
	assert.Error(t, err)
}

func TestClosestName(t *testing.T) {
	options := []string{"sand", "stag", "prod"}
	tests := []struct {
		typed string
		want  string
		found bool
	}{
		{"sandd", "sand", true},
		{"pord", "prod", true},
		{"prd", "prod", true},
		{"produccion", "prod", true},
		{"SAND", "sand", true},
		{"nope", "", false},
		{"zzzzzz", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.typed, func(t *testing.T) {
			got, found := ClosestName(tt.typed, options)

			assert.Equal(t, tt.found, found)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRecents_LosIntentosAbiertosSonLosQueNoTienenResultadoNiFueronAbandonados(t *testing.T) {
	recents, _ := newRecents(t)
	require.NoError(t, recents.RememberAttempt("p", RecentAttempt{ID: idA, Status: AttemptSucceeded}))
	require.NoError(t, recents.RememberAttempt("p", RecentAttempt{ID: idB}))
	require.NoError(t, recents.RememberAttempt("p", RecentAttempt{ID: idC}))
	require.NoError(t, recents.RememberAttempt("p", RecentAttempt{ID: idC, Abandoned: true}))

	open, err := recents.OpenAttempts("p")

	require.NoError(t, err)
	require.Len(t, open, 1)
	assert.Equal(t, idB, open[0].ID, "el exitoso ya terminó y el abandonado ya no ocupa nada")
}

func TestRecents_UnIntentoAbandonadoNoVuelveAAbrirse(t *testing.T) {
	recents, store := newRecents(t)
	require.NoError(t, recents.RememberAttempt("p", RecentAttempt{ID: idA, Abandoned: true}))

	require.NoError(t, recents.RememberAttempt("p", RecentAttempt{ID: idA, Environment: "sand"}))

	assert.True(t, store.data["p"].Attempts[0].Abandoned, "una actualización posterior no deshace el abandono")
}
