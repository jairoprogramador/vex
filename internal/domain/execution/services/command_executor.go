package services

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"

	"github.com/jairoprogramador/vex/internal/domain/execution/ports"
	"github.com/jairoprogramador/vex/internal/domain/execution/vos"
)

type CommandExecutor struct {
	runner          ports.CommandRunner
	fileProcessor   ports.FileProcessor
	interpolator    ports.Interpolator
	outputExtractor ports.OutputExtractor
}

func NewCommandExecutor(
	runner ports.CommandRunner,
	fileProcessor ports.FileProcessor,
	interpolator ports.Interpolator,
	outputExtractor ports.OutputExtractor,
) ports.CommandExecutor {
	return &CommandExecutor{
		runner:          runner,
		fileProcessor:   fileProcessor,
		interpolator:    interpolator,
		outputExtractor: outputExtractor,
	}
}

func (ce *CommandExecutor) Execute(
	ctx context.Context,
	command vos.Command,
	currentVars vos.VariableSet,
	workspaceStep, workspaceShared string) *vos.ExecutionResult {

	workspaceMain := workspaceStep
	isShared := filepath.Base(command.Workdir()) == vos.SharedScope
	if isShared {
		workspaceMain = workspaceShared
	}

	absPathsFiles := make([]string, len(command.TemplateFiles()))
	for i, filePath := range command.TemplateFiles() {
		absPathsFiles[i] = filepath.Join(workspaceMain, command.Workdir(), filePath)
	}

	if err := ce.fileProcessor.Process(absPathsFiles, currentVars); err != nil {
		return &vos.ExecutionResult{Status: vos.Failure, Error: fmt.Errorf("falló al procesar las plantillas: %w", err)}
	}
	//defer ce.fileProcessor.Restore()

	interpolatedCmd, err := ce.interpolator.Interpolate(command.Cmd(), currentVars)
	if err != nil {
		return &vos.ExecutionResult{Status: vos.Failure, Error: fmt.Errorf("falló al interpolar el comando: %w", err)}
	}

	execDir := ""
	if command.Workdir() != "" {
		execDir = filepath.Join(workspaceMain, command.Workdir())
	}

	fmt.Printf("Ejecutando: '%s'\n", command.Name())
	cmdResult, err := ce.runner.Run(ctx, interpolatedCmd, execDir)
	if err != nil {
		return &vos.ExecutionResult{Status: vos.Failure, Error: fmt.Errorf("no se pudo iniciar el comando: %w", err)}
	}

	if cmdResult.ExitCode != 0 {
		return &vos.ExecutionResult{
			Status: vos.Failure,
			Logs:   cmdResult.CombinedOutput(),
			Error:  fmt.Errorf("el comando %s falló con código de salida %d", interpolatedCmd, cmdResult.ExitCode),
		}
	}

	if err := ce.checkProbes(cmdResult.NormalizedStdout, command.Outputs()); err != nil {
		return &vos.ExecutionResult{
			Status: vos.Failure,
			Logs:   cmdResult.CombinedOutput(),
			Error:  fmt.Errorf("falló al verificar las salidas: %w", err),
		}
	}

	extractedVars, err := ce.outputExtractor.ExtractVars(cmdResult.NormalizedStdout, command.Outputs())
	if err != nil {
		return &vos.ExecutionResult{
			Status: vos.Failure,
			Logs:   cmdResult.CombinedOutput(),
			Error:  fmt.Errorf("falló al extraer las salidas: %w", err),
		}
	}

	outputVars := vos.NewVariableSet()
	if len(extractedVars) > 0 {
		for name, value := range extractedVars {
			outputVar, err := vos.NewOutputVar(name, value.Value(), isShared)
			if err != nil {
				return &vos.ExecutionResult{
					Status: vos.Failure,
					Logs:   cmdResult.CombinedOutput(),
					Error:  fmt.Errorf("falló al crear la variable de salida '%s': %w", name, err),
				}
			}
			outputVars.Add(outputVar)
		}
	}

	return &vos.ExecutionResult{
		Status:     vos.Success,
		Logs:       cmdResult.CombinedOutput(),
		OutputVars: outputVars,
	}
}

func (ce *CommandExecutor) checkProbes(commandOutput string, outputs []vos.CommandOutput) error {
	for _, output := range outputs {
		re, err := regexp.Compile(output.Probe())
		if err != nil {
			return fmt.Errorf("expresión regular '%s' inválida: %w", output.Probe(), err)
		}

		matches := re.FindStringSubmatch(commandOutput)

		if len(matches) < 1 {
			return fmt.Errorf("expresión regular '%s' no encontró coincidencia en la salida", output.Probe())
		}
	}

	return nil
}
