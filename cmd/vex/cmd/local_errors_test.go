package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"

	"github.com/jairoprogramador/vex/internal/application"
)

func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"cancelado sale con 130", application.ErrAttemptCanceled, 130},
		{"cancelado envuelto sale con 130", fmt.Errorf("x: %w", application.ErrAttemptCanceled), 130},
		{"intento fallido sale con 1", application.ErrAttemptFailed, 1},
		{"error del motor sale con 1", &application.EngineError{Kind: application.EngineEnvironmentBusy}, 1},
		{"cualquier otro error sale con 1", errors.New("boom"), 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, exitCode(tt.err))
		})
	}
}

func TestReportLocalError_ImprimeUnaVezYSilenciaACobra(t *testing.T) {
	command := &cobra.Command{}
	var stderr bytes.Buffer
	command.SetErr(&stderr)

	reportLocalError(command, &application.EngineError{
		Kind: application.EngineEnvironmentBusy, Environment: "sand", AttemptID: "i9"})

	assert.True(t, command.SilenceErrors, "cobra no debe imprimirlo otra vez")
	assert.Contains(t, stderr.String(), "Error: El ambiente \"sand\"")
}

func TestReportLocalError_UnIntentoFallidoNoAñadeNada(t *testing.T) {
	command := &cobra.Command{}
	var stderr bytes.Buffer
	command.SetErr(&stderr)

	reportLocalError(command, application.ErrAttemptFailed)

	assert.True(t, command.SilenceErrors)
	assert.Empty(t, stderr.String())
}

func TestRootArgs_PasoYAmbienteOpcionalPeroNadaMas(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{"solo el paso", []string{"test"}, false},
		{"paso y ambiente", []string{"test", "sand"}, false},
		{"un tercer argumento sobra", []string{"test", "sand", "otro"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := vexCmd.Args(vexCmd, tt.args)

			if tt.wantErr {
				assert.ErrorContains(t, err, "sobran argumentos")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
