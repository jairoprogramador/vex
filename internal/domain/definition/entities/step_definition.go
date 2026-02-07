package entities

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jairoprogramador/vex/internal/domain/definition/vos"
)

type StepDefinition struct {
	name      vos.StepNameDefinition
	commands  []vos.CommandDefinition
	variables []vos.VariableDefinition
}

func NewStepDefinition(
	name vos.StepNameDefinition,
	commands []vos.CommandDefinition,
	variables []vos.VariableDefinition) (*StepDefinition, error) {

	if len(commands) == 0 {
		return nil, errors.New("un paso debe tener al menos un comando")
	}

	commandNames := make(map[string]bool)
	for _, cmd := range commands {
		name := strings.ToUpper(strings.ReplaceAll(cmd.Name(), " ", ""))
		cmdName := strings.ReplaceAll(cmd.Cmd(), " ", "")
		workdir := strings.ToUpper(strings.ReplaceAll(cmd.Workdir(), " ", ""))

		uniqueKey := fmt.Sprintf("%s-%s-%s", name, cmdName, workdir)
		if commandNames[uniqueKey] {
			return nil, errors.New("comando duplicados dentro del mismo paso: " + uniqueKey)
		}
		commandNames[uniqueKey] = true
	}

	variablesNames := make(map[string]bool)
	for _, variable := range variables {
		name := strings.ReplaceAll(variable.Name(), " ", "")
		if variablesNames[name] {
			return nil, errors.New("variable duplicada: " + name)
		}
		variablesNames[name] = true
	}

	return &StepDefinition{
		name:      name,
		commands:  commands,
		variables: variables,
	}, nil
}

func (s *StepDefinition) NameDef() vos.StepNameDefinition {
	return s.name
}

func (s *StepDefinition) CommandsDef() []vos.CommandDefinition {
	return s.commands
}

func (s *StepDefinition) VariablesDef() []vos.VariableDefinition {
	return s.variables
}
