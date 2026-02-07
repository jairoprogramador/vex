package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/jairoprogramador/vex/internal/domain/definition/aggregates"
	"github.com/jairoprogramador/vex/internal/domain/definition/entities"
	"github.com/jairoprogramador/vex/internal/domain/definition/ports"
	"github.com/jairoprogramador/vex/internal/domain/definition/vos"
)

type PlanBuilder struct {
	reader ports.DefinitionReader
}

func NewPlanBuilder(reader ports.DefinitionReader) *PlanBuilder {
	return &PlanBuilder{reader: reader}
}

func (b *PlanBuilder) Build(
	ctx context.Context,
	templatePath, finalStepName, envName string) (*aggregates.ExecutionPlanDefinition, error) {
	// 1. Validar y obtener el entorno
	environment, err := b.resolveEnvironment(ctx, templatePath, envName)
	if err != nil {
		return nil, err
	}
	// 2. Obtener y filtrar los pasos
	stepsToExecute, err := b.resolveSteps(ctx, templatePath, finalStepName)
	if err != nil {
		return nil, err
	}

	// 3. Ensamblar cada paso con sus comandos y variables
	assembledSteps := make([]*entities.StepDefinition, 0, len(stepsToExecute))
	for _, stepName := range stepsToExecute {
		step, err := b.assembleStep(ctx, templatePath, stepName, environment)
		if err != nil {
			return nil, fmt.Errorf("error al ensamblar el paso '%s': %w", stepName.Name(), err)
		}
		assembledSteps = append(assembledSteps, step)
	}

	// 4. Crear y devolver el agregado raíz
	return aggregates.NewExecutionPlanDefinition(environment, assembledSteps)
}

func (b *PlanBuilder) resolveEnvironment(ctx context.Context, templatePath, envName string) (vos.EnvironmentDefinition, error) {
	environments, err := b.reader.ReadEnvironments(ctx, filepath.Join(templatePath, "environments.yaml"))
	if err != nil {
		return vos.EnvironmentDefinition{}, fmt.Errorf("no se pudieron leer los entornos: %w", err)
	}
	if len(environments) == 0 {
		return vos.EnvironmentDefinition{}, errors.New("no hay entornos definidos en environments.yaml")
	}

	if envName == "" {
		return environments[0], nil
	}

	for _, env := range environments {
		if env.String() == envName {
			return env, nil
		}
	}

	return vos.EnvironmentDefinition{}, fmt.Errorf("el entorno '%s' no es válido", envName)
}

func (b *PlanBuilder) resolveSteps(ctx context.Context, templatePath, finalStepName string) ([]vos.StepNameDefinition, error) {
	allStepNames, err := b.reader.ReadStepNames(ctx, filepath.Join(templatePath, "steps"))
	if err != nil {
		return nil, fmt.Errorf("no se pudieron leer los pasos: %w", err)
	}
	// Asegurarse de que los pasos están ordenados
	sort.Slice(allStepNames, func(i, j int) bool {
		return allStepNames[i].Order() < allStepNames[j].Order()
	})

	finalStepIndex := -1
	for i, s := range allStepNames {
		if s.Name() == finalStepName {
			finalStepIndex = i
			break
		}
	}

	if finalStepIndex == -1 {
		return nil, fmt.Errorf("el paso final '%s' no se encontró", finalStepName)
	}

	return allStepNames[:finalStepIndex+1], nil
}

func (b *PlanBuilder) assembleStep(ctx context.Context,
	templatePath string, stepName vos.StepNameDefinition,
	env vos.EnvironmentDefinition) (*entities.StepDefinition, error) {

	commandsPath := filepath.Join(templatePath, "steps", stepName.FullName(), "commands.yaml")
	variablesPath := filepath.Join(templatePath, "variables", env.String(), stepName.Name()+".yaml")

	commands, err := b.reader.ReadCommands(ctx, commandsPath)
	if err != nil {
		return nil, fmt.Errorf("error al leer los comandos: %w", err)
	}

	variables, err := b.reader.ReadVariables(ctx, variablesPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("error al leer las variables: %w", err)
		}
		variables = []vos.VariableDefinition{}
	}

	return entities.NewStepDefinition(stepName, commands, variables)
}
