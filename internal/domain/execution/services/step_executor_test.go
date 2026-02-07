package services_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jairoprogramador/vex/internal/domain/execution/entities"
	"github.com/jairoprogramador/vex/internal/domain/execution/ports"
	"github.com/jairoprogramador/vex/internal/domain/execution/services"
	"github.com/jairoprogramador/vex/internal/domain/execution/vos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockInterpolator simula el servicio de interpolación para las pruebas.
type mockInterpolator struct{}

func (m *mockInterpolator) Interpolate(input string, vars vos.VariableSet) (string, error) {
	result := input
	stringVars := vars.ToStringMap() // Usamos ToStringMap para la interpolación
	for k, v := range stringVars {
		placeholder := fmt.Sprintf("${var.%s}", k)
		if strings.Contains(result, placeholder) {
			result = strings.ReplaceAll(result, placeholder, v)
		}
	}
	if strings.Contains(result, "${var.") {
		return "", fmt.Errorf("variable no encontrada")
	}
	return result, nil
}

type MockStepCommandExecutor struct {
	mock.Mock
}

// Aseguramos que el mock cumple con la interfaz.
var _ ports.CommandExecutor = &MockStepCommandExecutor{}

func (m *MockStepCommandExecutor) Execute(
	ctx context.Context,
	command vos.Command,
	currentVars vos.VariableSet,
	workspaceStep, workspaceShared string,
) *vos.ExecutionResult {
	args := m.Called(ctx, command, currentVars, workspaceStep)
	return args.Get(0).(*vos.ExecutionResult)
}

func TestStepExecutor_Execute_Success(t *testing.T) {
	cmdExecutor := new(MockStepCommandExecutor)
	interpolator := &mockInterpolator{}
	resolver := services.NewVariableResolver(interpolator)
	stepExecutor := services.NewStepExecutor(cmdExecutor, resolver)

	varName1, varValue1 := "var1", "val1"
	varInitName1, varInitValue1 := "init", "true"
	pathRoot := "/root"
	log1, log2 := "log1", "log2"

	output1, _ := vos.NewCommandOutput(varName1, varValue1)
	cmd1, _ := vos.NewCommand("cmd1", "echo "+log1, vos.WithOutputs([]vos.CommandOutput{output1}))
	cmd2, _ := vos.NewCommand("cmd2", "echo "+log2)

	step, _ := entities.NewStep("test-step",
		entities.WithCommands([]vos.Command{cmd1, cmd2}),
		entities.WithWorkspaceStep(pathRoot),
	)

	initialVars := vos.NewVariableSet()
	initVar, _ := vos.NewOutputVar(varInitName1, varInitValue1, false)
	initialVars.Add(initVar)

	// Preparamos las variables esperadas para la PRIMERA llamada, incluyendo step_workdir
	expectedVarsForCmd1 := initialVars.Clone()
	stepWorkdirVar, _ := vos.NewOutputVar("step_workdir", pathRoot, false)
	expectedVarsForCmd1.Add(stepWorkdirVar)

	cmdExecutor.On("Execute", mock.Anything, cmd1, expectedVarsForCmd1, pathRoot).Return(&vos.ExecutionResult{
		Status:     vos.Success,
		Logs:       log1,
		OutputVars: vos.VariableSet{"var1": newVar(varName1, varValue1)},
	}).Once()

	// Preparamos las variables esperadas para la SEGUNDA llamada
	expectedVarsForCmd2 := expectedVarsForCmd1.Clone()
	expectedVarsForCmd2.Add(newVar(varName1, varValue1))

	cmdExecutor.On("Execute", mock.Anything, cmd2, expectedVarsForCmd2, pathRoot).Return(&vos.ExecutionResult{
		Status:     vos.Success,
		Logs:       log2,
		OutputVars: vos.NewVariableSet(), // El segundo comando no devuelve variables
	}).Once()

	// Act
	result, err := stepExecutor.Execute(context.Background(), &step, initialVars)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, vos.Success, result.Status)
	assert.Contains(t, result.Logs, log1)
	assert.Contains(t, result.Logs, log2)

	// El resultado final debe contener solo las variables del primer comando que generó output
	finalExpectedVars := vos.NewVariableSet()
	finalExpectedVars.Add(newVar(varName1, varValue1))
	assert.True(t, finalExpectedVars.Equals(result.OutputVars))

	assert.NoError(t, result.Error)
	cmdExecutor.AssertExpectations(t)
}

func TestStepExecutor_Execute_StopsOnFailure(t *testing.T) {
	cmdExecutor := new(MockStepCommandExecutor)
	interpolator := &mockInterpolator{}
	resolver := services.NewVariableResolver(interpolator)
	stepExecutor := services.NewStepExecutor(cmdExecutor, resolver)

	cmd1, _ := vos.NewCommand("cmd1", "failing command")
	cmd2, _ := vos.NewCommand("cmd2", "should not run")
	step, _ := entities.NewStep("fail-step", entities.WithCommands([]vos.Command{cmd1, cmd2}))
	failError := errors.New("command failed")

	// Mockea la primera llamada para que falle
	cmdExecutor.On("Execute", mock.Anything, cmd1, mock.Anything, mock.Anything).Return(&vos.ExecutionResult{
		Status: vos.Failure,
		Error:  failError,
		Logs:   "error log",
	}).Once()

	// Act
	result, err := stepExecutor.Execute(context.Background(), &step, vos.NewVariableSet())

	// Assert
	require.NoError(t, err)
	assert.Equal(t, vos.Failure, result.Status)
	assert.ErrorIs(t, result.Error, failError)
	assert.Contains(t, result.Logs, "error log")
	cmdExecutor.AssertExpectations(t)
	cmdExecutor.AssertNumberOfCalls(t, "Execute", 1)
}

func TestStepExecutor_Execute_StopsOnIrrecoverableError(t *testing.T) {
	cmdExecutor := new(MockStepCommandExecutor)
	interpolator := &mockInterpolator{}
	resolver := services.NewVariableResolver(interpolator)
	stepExecutor := services.NewStepExecutor(cmdExecutor, resolver)

	cmd1, _ := vos.NewCommand("cmd1", "failing command")
	step, _ := entities.NewStep("fail-step", entities.WithCommands([]vos.Command{cmd1}))
	irrecoverableError := errors.New("irrecoverable")

	cmdExecutor.On("Execute", mock.Anything, cmd1, mock.Anything, mock.Anything).Return(&vos.ExecutionResult{
		Status: vos.Failure, // El status debe ser Failure si hay un Error
		Error:  irrecoverableError,
	}).Once()

	// Act
	result, err := stepExecutor.Execute(context.Background(), &step, vos.NewVariableSet())

	// Assert
	require.NoError(t, err)
	assert.Equal(t, vos.Failure, result.Status)
	assert.ErrorIs(t, result.Error, irrecoverableError)
	cmdExecutor.AssertExpectations(t)
}

// Helper para crear OutputVar de forma segura en tests
func newVar(name, value string) vos.OutputVar {
	v, err := vos.NewOutputVar(name, value, false)
	if err != nil {
		panic(err)
	}
	return v
}
