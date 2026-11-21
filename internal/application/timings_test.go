package application

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tickingClock avanza un segundo cada vez que se le pregunta la hora.
func tickingClock() func() time.Time {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	return func() time.Time {
		now = now.Add(time.Second)
		return now
	}
}

func TestPhaseTimer_MideFasesYPasos(t *testing.T) {
	timer := NewPhaseTimer(tickingClock())

	stop := timer.Phase("login")
	stop()
	stop = timer.Phase("conexion")
	stop()
	stop = timer.Phase("login") // una fase que se repite acumula
	stop()
	timer.Observe(EngineEvent{Kind: StepFinished, Step: "test", StepStatus: StepReused}) // sin comienzo: no se mide
	timer.Observe(EngineEvent{Kind: StepStarted, Step: "deploy"})
	timer.Observe(EngineEvent{Kind: CommandFinished, Step: "deploy"})
	timer.Observe(EngineEvent{Kind: StepFinished, Step: "deploy", StepStatus: StepExecuted})

	run := timer.Timings()

	assert.Equal(t, []PhaseTiming{{Name: "login", Duration: 2 * time.Second}, {Name: "conexion", Duration: time.Second}}, run.Phases)
	assert.Equal(t, []StepTiming{{Name: "deploy", Status: StepExecuted, Duration: time.Second}}, run.Steps)
	assert.Equal(t, 9*time.Second, run.Total)
}

func TestPhaseTimer_NilNoMideNada(t *testing.T) {
	var timer *PhaseTimer

	timer.Phase("login")()
	timer.Observe(EngineEvent{Kind: StepStarted, Step: "x"})

	assert.Zero(t, timer.Elapsed())
}

func TestAppendTimings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vex", "tiempos.jsonl")
	run := RunTimings{
		At: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC), Project: "p1", Environment: "sand", Step: "deploy",
		Status: AttemptSucceeded, Total: 482 * time.Second,
		Phases: []PhaseTiming{{Name: "login", Duration: 1500 * time.Millisecond}},
		Steps:  []StepTiming{{Name: "deploy", Status: StepExecuted, Duration: 90 * time.Second}},
	}

	require.NoError(t, AppendTimings(path, run))
	require.NoError(t, AppendTimings(path, run))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	require.Len(t, lines, 2, "una línea por ejecución")
	assert.JSONEq(t, `{"at":"2026-11-20T12:00:00Z","project":"p1","environment":"sand","step":"deploy","status":"succeeded",
		"total_ms":482000,"phases_ms":{"login":1500},"steps":[{"name":"deploy","status":"executed","ms":90000}]}`, lines[0])
	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &decoded))

	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
}

func TestAppendTimings_SiNoSePuedeEscribirDevuelveElError(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "archivo")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))

	err := AppendTimings(filepath.Join(blocker, "tiempos.jsonl"), RunTimings{})

	assert.ErrorContains(t, err, "guardar los tiempos")
}
