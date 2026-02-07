package aggregates

import (
	"time"

	"github.com/jairoprogramador/vex/internal/domain/state/vos"
)

type StateEntry struct {
	code        vos.Fingerprint
	instruction vos.Fingerprint
	environment vos.Environment
	vars        vos.Fingerprint
	createdAt   time.Time
}

func NewStateEntry(code, instruction, vars vos.Fingerprint, environment vos.Environment) *StateEntry {
	return &StateEntry{
		code:        code,
		instruction: instruction,
		environment: environment,
		vars:        vars,
		createdAt:   time.Now().UTC(),
	}
}

func (se StateEntry) Code() vos.Fingerprint {
	return se.code
}

func (se StateEntry) Instruction() vos.Fingerprint {
	return se.instruction
}

func (se StateEntry) Environment() vos.Environment {
	return se.environment
}

func (se StateEntry) Vars() vos.Fingerprint {
	return se.vars
}

func (se StateEntry) CreatedAt() time.Time {
	return se.createdAt
}

func (se *StateEntry) SetCreatedAt(t time.Time) {
	se.createdAt = t
}

func (se StateEntry) Equals(other StateEntry) bool {
	return se.code.Equals(other.code) &&
		se.instruction.Equals(other.instruction) &&
		se.environment.Equals(other.environment) &&
		se.vars.Equals(other.vars) &&
		se.createdAt.Equal(other.createdAt)
}
