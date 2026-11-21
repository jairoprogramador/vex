package application

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// PhaseTimer mide cuánto tarda cada fase de una ejecución vista desde la CLI: lo que el motor no ve (iniciar
// sesión, consultar la conexión, esperar la imagen) y, con sus eventos, cada paso. Un *PhaseTimer nil no mide
// nada: quien no quiere tiempos no necesita uno.
type PhaseTimer struct {
	now     func() time.Time
	started time.Time
	names   []string // fases en el orden en que empezaron
	phases  map[string]time.Duration
	steps   []StepTiming
	running map[string]time.Time // pasos en curso
}

// StepTiming es lo que tardó un paso que el motor ejecutó.
type StepTiming struct {
	Name     string
	Status   StepStatus
	Duration time.Duration
}

// NewPhaseTimer empieza a contar el total desde ahora.
func NewPhaseTimer(now func() time.Time) *PhaseTimer {
	return &PhaseTimer{now: now, started: now(), phases: map[string]time.Duration{}, running: map[string]time.Time{}}
}

// Phase empieza a medir una fase; la función devuelta la termina. Una fase que se repite acumula.
func (t *PhaseTimer) Phase(name string) (stop func()) {
	if t == nil {
		return func() {}
	}
	if _, known := t.phases[name]; !known {
		t.names = append(t.names, name)
		t.phases[name] = 0
	}
	start := t.now()
	return func() { t.phases[name] += t.now().Sub(start) }
}

// Observe mide los pasos con los eventos del motor. Un paso reutilizado no tiene comienzo: no se mide.
func (t *PhaseTimer) Observe(event EngineEvent) {
	if t == nil {
		return
	}
	switch event.Kind {
	case StepStarted:
		t.running[event.Step] = t.now()
	case StepFinished:
		if start, ok := t.running[event.Step]; ok {
			delete(t.running, event.Step)
			t.steps = append(t.steps, StepTiming{Name: event.Step, Status: event.StepStatus, Duration: t.now().Sub(start)})
		}
	}
}

// Elapsed es lo transcurrido desde que se creó el cronómetro.
func (t *PhaseTimer) Elapsed() time.Duration {
	if t == nil {
		return 0
	}
	return t.now().Sub(t.started)
}

// RunTimings son los tiempos de una ejecución, tal como se guardan.
type RunTimings struct {
	At          time.Time
	Project     string
	Environment string
	Step        string
	Status      AttemptStatus
	Total       time.Duration
	// Phases son las fases propias de la CLI, en el orden en que ocurrieron.
	Phases []PhaseTiming
	Steps  []StepTiming
}

type PhaseTiming struct {
	Name     string
	Duration time.Duration
}

// Timings resume lo medido hasta ahora.
func (t *PhaseTimer) Timings() RunTimings {
	run := RunTimings{At: t.started, Total: t.Elapsed(), Steps: t.steps}
	for _, name := range t.names {
		run.Phases = append(run.Phases, PhaseTiming{Name: name, Duration: t.phases[name]})
	}
	return run
}

// timingsLine es la forma en disco de RunTimings: una línea JSON con las duraciones en milisegundos.
type timingsLine struct {
	At          time.Time        `json:"at"`
	Project     string           `json:"project"`
	Environment string           `json:"environment"`
	Step        string           `json:"step"`
	Status      AttemptStatus    `json:"status"`
	TotalMS     int64            `json:"total_ms"`
	Phases      map[string]int64 `json:"phases_ms"`
	Steps       []stepLine       `json:"steps"`
}

type stepLine struct {
	Name   string     `json:"name"`
	Status StepStatus `json:"status"`
	MS     int64      `json:"ms"`
}

// AppendTimings añade los tiempos de una ejecución al registro local (JSON Lines, solo para su dueño). El
// registro sirve para medir; no guarda nada de la nube ni salidas de comandos.
func AppendTimings(path string, run RunTimings) error {
	line := timingsLine{
		At: run.At.UTC(), Project: run.Project, Environment: run.Environment, Step: run.Step, Status: run.Status,
		TotalMS: run.Total.Milliseconds(), Phases: make(map[string]int64, len(run.Phases)), Steps: []stepLine{},
	}
	for _, phase := range run.Phases {
		line.Phases[phase.Name] = phase.Duration.Milliseconds()
	}
	for _, step := range run.Steps {
		line.Steps = append(line.Steps, stepLine{Name: step.Name, Status: step.Status, MS: step.Duration.Milliseconds()})
	}
	data, err := json.Marshal(line)
	if err != nil {
		return fmt.Errorf("guardar los tiempos: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("guardar los tiempos: %w", err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("guardar los tiempos: %w", err)
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		return fmt.Errorf("guardar los tiempos: %w", err)
	}
	return file.Close()
}
