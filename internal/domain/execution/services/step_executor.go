package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/jairoprogramador/vex/internal/domain/execution/entities"
	"github.com/jairoprogramador/vex/internal/domain/execution/ports"
	"github.com/jairoprogramador/vex/internal/domain/execution/vos"
)

type StepExecutor struct {
	commandExecutor  ports.CommandExecutor
	variableResolver ports.VariableResolver
}

func NewStepExecutor(
	commandExecutor ports.CommandExecutor,
	variableResolver ports.VariableResolver) *StepExecutor {
	return &StepExecutor{
		commandExecutor:  commandExecutor,
		variableResolver: variableResolver,
	}
}

func (se *StepExecutor) Execute(
	ctx context.Context,
	step *entities.Step,
	initialVars vos.VariableSet) (*vos.ExecutionResult, error) {

	cumulativeLogs := &strings.Builder{}
	cumulativeVars := initialVars.Clone()

	resolvedStepVars, err := se.variableResolver.Resolve(cumulativeVars, step.Variables())
	if err != nil {
		err := fmt.Errorf("error al resolver las variables del step '%s': %w", step.Name(), err)
		return &vos.ExecutionResult{
			Status:     vos.Failure,
			OutputVars: vos.NewVariableSet(),
			Error:      err,
		}, err
	}
	cumulativeVars.AddAll(resolvedStepVars)

	stepWorkdir := step.WorkspaceStep()
	stepWorkdirVar, _ := vos.NewOutputVar("step_workdir", stepWorkdir, false)
	cumulativeVars.Add(stepWorkdirVar)

	sharedWorkdir := step.WorkspaceShared()
	sharedWorkdirVar, _ := vos.NewOutputVar("shared_workdir", sharedWorkdir, false)
	cumulativeVars.Add(sharedWorkdirVar)

	var finalError error
	finalStatus := vos.Success

	outputVars := vos.NewVariableSet()

	for _, command := range step.Commands() {
		cmdResult := se.commandExecutor.Execute(ctx, command, cumulativeVars, stepWorkdir, sharedWorkdir)

		if cmdResult.Logs != "" {
			cumulativeLogs.WriteString(fmt.Sprintf("  - comando: '%s'\n", command.Name()))
			cumulativeLogs.WriteString(cmdResult.Logs)
			cumulativeLogs.WriteString("\n")
		}

		if cmdResult.Error != nil || cmdResult.Status == vos.Failure {
			finalError = fmt.Errorf("el comando '%s' falló", command.Name())
			if cmdResult.Error != nil {
				finalError = fmt.Errorf("el comando '%s' falló: %w", command.Name(), cmdResult.Error)
			}
			finalStatus = vos.Failure
			break
		}

		cumulativeVars.AddAll(cmdResult.OutputVars)
		outputVars.AddAll(cmdResult.OutputVars)
	}

	return &vos.ExecutionResult{
		Status:     finalStatus,
		Logs:       cumulativeLogs.String(),
		OutputVars: outputVars,
		Error:      finalError,
	}, nil
}
