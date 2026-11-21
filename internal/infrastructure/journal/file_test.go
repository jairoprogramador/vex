package journal

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jairoprogramador/vex/internal/application"
)

func TestFile_AnadeUnaLineaPorConstanciaYSoloLaLeeSuDueno(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nube", "aceptaciones.jsonl")
	journal := NewFile(path)
	first := application.AcceptanceRecord{
		At: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC), Action: "bootstrap", Outcome: "accepted",
		ProjectID: "b5a1c2d4-0000-4000-8000-0000000000b1", Environment: "sand", Contract: "azure/v1",
		Account:        []application.LabeledValue{{Label: "Suscripción", Value: "Pruebas"}},
		PipelineCommit: "0123abc", Fingerprint: "c0c0", Region: "eastus2",
		ElevatedGrants: []application.PlannedGrant{{Identity: "deploy", Role: "Owner", Scope: "toda la suscripción", Elevated: true, Reason: "controla permisos"}},
	}

	require.NoError(t, journal.Record(first))
	require.NoError(t, journal.Record(application.AcceptanceRecord{Action: "disconnect", Outcome: "trust_removed", Environment: "sand"}))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	dir, err := os.Stat(filepath.Dir(path))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dir.Mode().Perm())

	file, err := os.Open(path)
	require.NoError(t, err)
	defer file.Close()
	var records []application.AcceptanceRecord
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var record application.AcceptanceRecord
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &record))
		records = append(records, record)
	}
	require.Len(t, records, 2)
	assert.Equal(t, first, records[0])
	assert.Equal(t, "disconnect", records[1].Action)
}

func TestFile_SiNoPuedeEscribirLoDice(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "archivo")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))

	err := NewFile(filepath.Join(blocker, "aceptaciones.jsonl")).Record(application.AcceptanceRecord{})

	assert.Error(t, err)
}
