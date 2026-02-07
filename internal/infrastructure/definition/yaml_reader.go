package definition

import (
	"context"
	"fmt"
	"os"

	"github.com/jairoprogramador/vex/internal/domain/definition/ports"
	"github.com/jairoprogramador/vex/internal/domain/definition/vos"
	"github.com/jairoprogramador/vex/internal/infrastructure/definition/dto"
	"gopkg.in/yaml.v3"
)

// YamlDefinitionReader implementa la interfaz DefinitionReader.
type YamlDefinitionReader struct{}

// NewYamlDefinitionReader crea una nueva instancia de YamlDefinitionReader.
func NewYamlDefinitionReader() ports.DefinitionReader {
	return &YamlDefinitionReader{}
}

// ReadEnvironments lee y parsea el archivo environments.yaml.
func (r *YamlDefinitionReader) ReadEnvironments(ctx context.Context, sourcePath string) ([]vos.EnvironmentDefinition, error) {
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []vos.EnvironmentDefinition{}, nil
		}
		return nil, err
	}

	var dtos []dto.EnvironmentDTO
	if err := yaml.Unmarshal(data, &dtos); err != nil {
		return nil, fmt.Errorf("error al parsear YAML de entornos: %w", err)
	}

	environments := make([]vos.EnvironmentDefinition, 0, len(dtos))
	for _, envDTO := range dtos {
		env, err := vos.NewEnvironment(envDTO.Value, envDTO.Name)
		if err != nil {
			return nil, fmt.Errorf("entorno inválido en el archivo de definición: %w", err)
		}
		environments = append(environments, env)
	}
	return environments, nil
}

// ReadStepNames escanea el directorio de pasos y extrae los nombres.
func (r *YamlDefinitionReader) ReadStepNames(ctx context.Context, stepsDir string) ([]vos.StepNameDefinition, error) {
	files, err := os.ReadDir(stepsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []vos.StepNameDefinition{}, nil
		}
		return nil, err
	}

	stepNames := make([]vos.StepNameDefinition, 0)
	for _, file := range files {
		if file.IsDir() {
			stepName, err := vos.NewStepNameDefinition(file.Name())
			if err == nil { // Solo añadimos los que tienen el formato correcto
				stepNames = append(stepNames, stepName)
			}
		}
	}
	return stepNames, nil
}

// ReadCommands lee y parsea un archivo commands.yaml.
func (r *YamlDefinitionReader) ReadCommands(ctx context.Context, commandsFilePath string) ([]vos.CommandDefinition, error) {
	data, err := os.ReadFile(commandsFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []vos.CommandDefinition{}, nil
		}
		return nil, err
	}

	var dtos []dto.CommandDTO
	if err := yaml.Unmarshal(data, &dtos); err != nil {
		return nil, fmt.Errorf("error al parsear YAML de comandos '%s': %w", commandsFilePath, err)
	}

	commands := make([]vos.CommandDefinition, 0, len(dtos))
	for _, cmdDTO := range dtos {
		// Mapeo de DTOs de output anidados a VOs de output
		outputs := make([]vos.OutputDefinition, 0, len(cmdDTO.Outputs))
		for _, outDTO := range cmdDTO.Outputs {
			out, err := vos.NewOutputDefinition(outDTO.Name, outDTO.Description, outDTO.Probe)
			if err != nil {
				return nil, fmt.Errorf("output inválido en comando '%s': %w", cmdDTO.Name, err)
			}
			outputs = append(outputs, out)
		}

		cmd, err := vos.NewCommandDefinition(
			cmdDTO.Name,
			cmdDTO.Cmd,
			vos.WithDescription(cmdDTO.Description),
			vos.WithWorkdir(cmdDTO.Workdir),
			vos.WithTemplateFiles(cmdDTO.TemplateFiles),
			vos.WithOutputs(outputs),
		)
		if err != nil {
			return nil, fmt.Errorf("comando inválido '%s': %w", cmdDTO.Name, err)
		}
		commands = append(commands, cmd)
	}
	return commands, nil
}

// ReadVariables lee y parsea un archivo de variables.
func (r *YamlDefinitionReader) ReadVariables(ctx context.Context, variablesFilePath string) ([]vos.VariableDefinition, error) {
	data, err := os.ReadFile(variablesFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []vos.VariableDefinition{}, nil
		}
		return nil, err
	}

	var variableDTOs []dto.Variable
	err = yaml.Unmarshal(data, &variableDTOs)
	if err != nil {
		return nil, fmt.Errorf("error al parsear YAML de variables '%s': %w", variablesFilePath, err)
	}

	variables := make([]vos.VariableDefinition, 0, len(variableDTOs))
	for _, vDTO := range variableDTOs {
		variable, err := vos.NewVariableDefinition(vDTO.Name, vDTO.Value)
		if err != nil {
			return nil, fmt.Errorf("variable inválida '%s' en archivo '%s': %w", vDTO.Name, variablesFilePath, err)
		}
		variables = append(variables, variable)
	}
	return variables, nil
}
