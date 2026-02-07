package aggregates

import (
	"testing"
	"time"

	"github.com/jairoprogramador/vex/internal/domain/state/vos"
)

func newFingerprint(value string) vos.Fingerprint {
	fp, _ := vos.NewFingerprint(value)
	return fp
}

func newEnv(value string) vos.Environment {
	env, _ := vos.NewEnvironment(value)
	return env
}

func TestNewStateTable(t *testing.T) {
	stateTable := NewStateTable(vos.StepTest)
	if stateTable == nil {
		t.Fatal("NewStateTable() devolvió nil")
	}
	if len(stateTable.entries) != 0 {
		t.Errorf("Se esperaba una tabla de estado vacía, pero tiene %d entradas", len(stateTable.entries))
	}
	if stateTable.Name() != vos.StepTest {
		t.Errorf("Se esperaba que el nombre fuera %s, pero se obtuvo %s", vos.StepTest, stateTable.Name())
	}
}

func TestStateTable_AddEntry_MaintainsOrder(t *testing.T) {
	st := NewStateTable(vos.StepTest)
	env := newEnv("dev")
	now := time.Now().UTC()

	// Crear entradas con timestamps desordenados
	entry2 := NewStateEntry(newFingerprint("c2"), newFingerprint("i2"), newFingerprint("v2"), env)
	entry2.createdAt = now.Add(2 * time.Second)

	entry1 := NewStateEntry(newFingerprint("c1"), newFingerprint("i1"), newFingerprint("v1"), env)
	entry1.createdAt = now.Add(1 * time.Second)

	entry3 := NewStateEntry(newFingerprint("c3"), newFingerprint("i3"), newFingerprint("v3"), env)
	entry3.createdAt = now.Add(3 * time.Second)

	// Añadir en desorden
	st.AddEntry(entry2)
	st.AddEntry(entry1)
	st.AddEntry(entry3)

	if len(st.Entries()) != 3 {
		t.Fatalf("Se esperaban 3 entradas, pero se obtuvieron %d", len(st.Entries()))
	}

	// Verificar que el orden interno es correcto
	if !st.Entries()[0].CreatedAt().Equal(entry1.createdAt) {
		t.Errorf("La primera entrada debería ser entry1, pero fue %v", st.Entries()[0])
	}
	if !st.Entries()[1].CreatedAt().Equal(entry2.createdAt) {
		t.Errorf("La segunda entrada debería ser entry2, pero fue %v", st.Entries()[1])
	}
	if !st.Entries()[2].CreatedAt().Equal(entry3.createdAt) {
		t.Errorf("La tercera entrada debería ser entry3, pero fue %v", st.Entries()[2])
	}
}

func TestStateTable_AddEntry_MaxEntries(t *testing.T) {
	st := NewStateTable(vos.StepTest)
	env := newEnv("dev")

	// Llenar la tabla con N entradas
	baseTime := time.Now().UTC()
	for i := 0; i < maxEntries; i++ {
		entry := NewStateEntry(newFingerprint("c"), newFingerprint("i"), newFingerprint("v"), env)
		entry.createdAt = baseTime.Add(time.Duration(i) * time.Second)
		st.AddEntry(entry)
	}

	if len(st.Entries()) != maxEntries {
		t.Fatalf("Se esperaba la tabla llena con %d entradas, pero tiene %d", maxEntries, len(st.Entries()))
	}

	oldestEntryTimestamp := st.Entries()[0].CreatedAt() // Timestamp de la entrada más antigua

	// Añadir una entrada más nueva, que debería causar la expulsión de la más antigua
	newestEntry := NewStateEntry(newFingerprint("new"), newFingerprint("new"), newFingerprint("new"), env)
	newestEntry.createdAt = baseTime.Add(time.Duration(maxEntries) * time.Second)
	st.AddEntry(newestEntry)

	if len(st.Entries()) != maxEntries {
		t.Errorf("Se esperaba que la tabla mantuviera %d entradas, pero tiene %d", maxEntries, len(st.Entries()))
	}

	// Verificar que la entrada más antigua fue eliminada
	for _, entry := range st.Entries() {
		if entry.CreatedAt().Equal(oldestEntryTimestamp) {
			t.Errorf("La entrada más antigua (Timestamp: %v) no fue eliminada", oldestEntryTimestamp)
		}
	}

	// Añadir una entrada más antigua, que no debería ser añadida (o debería ser añadida y expulsada inmediatamente)
	oldestEntry := NewStateEntry(newFingerprint("too_old"), newFingerprint("i"), newFingerprint("v"), env)
	oldestEntry.createdAt = baseTime.Add(-1 * time.Second)
	st.AddEntry(oldestEntry)

	// Verificar que la entrada "too_old" no está en la lista final
	for _, entry := range st.Entries() {
		if entry.Code().Equals(newFingerprint("too_old")) {
			t.Error("Una entrada más antigua que las existentes no debería permanecer en la tabla llena")
		}
	}
}

func TestLoadStateTable(t *testing.T) {
	env := newEnv("test")
	now := time.Now().UTC()

	t.Run("debería ordenar las entradas al cargar", func(t *testing.T) {
		// Entradas desordenadas
		entry2 := NewStateEntry(newFingerprint("c2"), newFingerprint("i2"), newFingerprint("v2"), env)
		entry2.createdAt = now.Add(2 * time.Second)
		entry1 := NewStateEntry(newFingerprint("c1"), newFingerprint("i1"), newFingerprint("v1"), env)
		entry1.createdAt = now.Add(1 * time.Second)

		entries := []*StateEntry{entry2, entry1}
		st := LoadStateTable(vos.StepDeploy, entries)

		if len(st.Entries()) != 2 {
			t.Fatalf("Se esperaban 2 entradas, pero se obtuvieron %d", len(st.Entries()))
		}
		if !st.Entries()[0].CreatedAt().Equal(entry1.createdAt) {
			t.Error("La tabla no se ordenó correctamente al cargar")
		}
	})

	t.Run("debería truncar las entradas si exceden maxEntries", func(t *testing.T) {
		// Crear más entradas que maxEntries
		var entries []*StateEntry
		for i := 0; i < maxEntries+2; i++ {
			e := NewStateEntry(newFingerprint("c"), newFingerprint("i"), newFingerprint("v"), env)
			e.createdAt = now.Add(time.Duration(i) * time.Second)
			entries = append(entries, e)
		}

		st := LoadStateTable(vos.StepDeploy, entries)

		if len(st.Entries()) != maxEntries {
			t.Errorf("La tabla no se truncó a maxEntries. Se esperaban %d, se obtuvieron %d", maxEntries, len(st.Entries()))
		}
		// Verificar que se quedó con las más recientes
		if !st.Entries()[0].CreatedAt().Equal(entries[2].createdAt) {
			t.Errorf("No se conservaron las entradas más recientes. Se esperaba que la más antigua fuera %v, pero fue %v", entries[2].createdAt, st.Entries()[0].CreatedAt())
		}
	})

	t.Run("debería funcionar con una lista de entradas vacía", func(t *testing.T) {
		entries := []*StateEntry{}
		st := LoadStateTable(vos.StepDeploy, entries)
		if len(st.Entries()) != 0 {
			t.Errorf("Se esperaba una tabla vacía, pero se obtuvieron %d entradas", len(st.Entries()))
		}
	})
}
