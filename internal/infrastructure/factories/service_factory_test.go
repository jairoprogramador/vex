package factories

import (
	"testing"

	"github.com/stretchr/testify/assert"

	app "github.com/jairoprogramador/vex/internal/application"
	"github.com/jairoprogramador/vex/internal/config"
)

func TestBuildRunner_ElModoLocalEsElPorDefecto(t *testing.T) {
	tests := []struct {
		name       string
		mode       config.ExecutionMode
		wantRemote bool
	}{
		{"local explícito", config.ModeLocal, false},
		{"sin modo (ModeUnset) cae en local", config.ModeUnset, false},
		{"el modo por defecto es local", config.DefaultMode, false},
		{"remoto solo si se pide explícitamente", config.ModeRemote, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner, err := NewServiceFactory().BuildRunner(tt.mode, true, true)

			assert.NoError(t, err)
			_, isRemote := runner.(*app.RemoteExecutorService)
			_, isLocal := runner.(*app.LocalExecutorService)
			assert.Equal(t, tt.wantRemote, isRemote)
			assert.Equal(t, !tt.wantRemote, isLocal)
		})
	}
}
