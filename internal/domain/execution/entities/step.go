package entities

import (
	"errors"

	"github.com/jairoprogramador/vex/internal/domain/execution/vos"
)

type Step struct {
	workspaceStep   string
	workspaceShared string
	name            string
	commands        []vos.Command
	variables       vos.VariableSet
}

type StepOption func(*Step)

func NewStep(name string, opts ...StepOption) (Step, error) {
	if name == "" {
		return Step{}, errors.New("el nombre del paso no puede estar vacío")
	}

	step := &Step{
		name: name,
	}

	for _, opt := range opts {
		opt(step)
	}

	if len(step.commands) == 0 {
		return Step{}, errors.New("un paso debe tener al menos un comando")
	}

	return *step, nil
}

func WithCommands(commands []vos.Command) StepOption {
	return func(s *Step) {
		s.commands = commands
	}
}

func WithVariables(variables vos.VariableSet) StepOption {
	return func(s *Step) {
		s.variables = variables
	}
}

func WithWorkspaceStep(workspaceRoot string) StepOption {
	return func(s *Step) {
		s.workspaceStep = workspaceRoot
	}
}

func WithWorkspaceShared(workspaceShared string) StepOption {
	return func(s *Step) {
		s.workspaceShared = workspaceShared
	}
}

func (sd Step) Name() string {
	return sd.name
}

func (sd Step) WorkspaceStep() string {
	return sd.workspaceStep
}

func (sd Step) WorkspaceShared() string {
	return sd.workspaceShared
}

func (sd Step) Commands() []vos.Command {
	commandsCopy := make([]vos.Command, len(sd.commands))
	copy(commandsCopy, sd.commands)
	return commandsCopy
}

func (sd Step) Variables() vos.VariableSet {
	variablesCopy := make(vos.VariableSet, len(sd.variables))
	for k, v := range sd.variables {
		variablesCopy[k] = v
	}
	return variablesCopy
}
