package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultMode_EsLocal(t *testing.T) {
	assert.Equal(t, ModeLocal, DefaultMode)
}

func TestMergeConfigs(t *testing.T) {
	cfg := func(m ExecutionMode) Config { return Config{Mode: m} }

	tests := []struct {
		name                  string
		global, user, project Config
		want                  ExecutionMode
	}{
		{"sin ningún nivel rige el default (local)", cfg(ModeUnset), cfg(ModeUnset), cfg(ModeUnset), ModeLocal},
		{"solo global", cfg(ModeRemote), cfg(ModeUnset), cfg(ModeUnset), ModeRemote},
		{"user gana a global", cfg(ModeRemote), cfg(ModeLocal), cfg(ModeUnset), ModeLocal},
		{"project gana a user y a global", cfg(ModeLocal), cfg(ModeLocal), cfg(ModeRemote), ModeRemote},
		{"un nivel vacío no pisa al de menor prioridad", cfg(ModeRemote), cfg(ModeUnset), cfg(ModeUnset), ModeRemote},
		{"se puede volver a local explícitamente", cfg(ModeRemote), cfg(ModeRemote), cfg(ModeLocal), ModeLocal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeConfigs(tt.global, tt.user, tt.project)

			assert.Equal(t, tt.want, got.Mode)
			assert.NotEqual(t, ModeUnset, got.Mode, "nunca devuelve ModeUnset")
		})
	}
}
