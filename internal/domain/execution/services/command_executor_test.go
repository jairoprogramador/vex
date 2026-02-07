package services_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/jairoprogramador/vex/internal/domain/execution/services"
	"github.com/jairoprogramador/vex/internal/domain/execution/vos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockCommandRunner
type MockCommandRunner struct{ mock.Mock }

func (m *MockCommandRunner) Run(ctx context.Context, command, workDir string) (*vos.CommandResult, error) {
	args := m.Called(ctx, command, workDir)
	if res := args.Get(0); res != nil {
		return res.(*vos.CommandResult), args.Error(1)
	}
	return nil, args.Error(1)
}

// MockFileProcessor
type MockFileProcessor struct{ mock.Mock }

func (m *MockFileProcessor) Process(absPathsFiles []string, vars vos.VariableSet) error {
	return m.Called(absPathsFiles, vars).Error(0)
}
func (m *MockFileProcessor) Restore() error {
	return m.Called().Error(0)
}

// MockInterpolator
type MockInterpolator struct{ mock.Mock }

func (m *MockInterpolator) Interpolate(input string, vars vos.VariableSet) (string, error) {
	args := m.Called(input, vars)
	return args.String(0), args.Error(1)
}

// MockOutputExtractor
type MockOutputExtractor struct{ mock.Mock }

func (m *MockOutputExtractor) ExtractVars(commandOutput string, outputs []vos.CommandOutput) (vos.VariableSet, error) {
	args := m.Called(commandOutput, outputs)
	if res := args.Get(0); res != nil {
		return res.(vos.VariableSet), args.Error(1)
	}
	return nil, args.Error(1)
}

func newOutputVarForTest(name, value string, isShared bool) vos.OutputVar {
	v, _ := vos.NewOutputVar(name, value, isShared)
	return v
}

func TestCommandExecutor_Execute_Success(t *testing.T) {
	runner, fileProcessor := new(MockCommandRunner), new(MockFileProcessor)
	interpolator, outputExtractor := new(MockInterpolator), new(MockOutputExtractor)
	executor := services.NewCommandExecutor(runner, fileProcessor, interpolator, outputExtractor)

	ctx := context.Background()
	pathRoot := "/app"

	nameCmd, myCmd, workdirCmd := "name_cmd", "my command", "workdir_cmd"
	interpolatedCmd := myCmd
	outputCmd := "output url: myapp.com"
	extractedVarsCmd := vos.NewVariableSet()
	extractedVarsCmd.Add(newOutputVarForTest("url", "myapp.com", false))

	cmd, _ := vos.NewCommand(nameCmd, myCmd, vos.WithWorkdir(workdirCmd))
	vars := vos.NewVariableSet()
	vars.Add(newOutputVarForTest("image", "nginx", false))

	fileProcessor.On("Process", mock.Anything, vars).Return(nil).Once()
	interpolator.On("Interpolate", cmd.Cmd(), vars).Return(interpolatedCmd, nil).Once()
	runner.On("Run", ctx, interpolatedCmd, filepath.Join(pathRoot, workdirCmd)).Return(&vos.CommandResult{ExitCode: 0, RawStdout: outputCmd, NormalizedStdout: outputCmd}, nil).Once()
	outputExtractor.On("ExtractVars", outputCmd, cmd.Outputs()).Return(extractedVarsCmd, nil).Once()
	fileProcessor.On("Restore").Return(nil).Once()

	// Act
	result := executor.Execute(ctx, cmd, vars, pathRoot, pathRoot)

	// Assert
	require.NotNil(t, result)
	assert.Equal(t, vos.Success, result.Status)
	assert.Equal(t, extractedVarsCmd, result.OutputVars)
	mock.AssertExpectationsForObjects(t, runner, fileProcessor, interpolator, outputExtractor)
}

func TestCommandExecutor_Execute_ErrorScenarios(t *testing.T) {
	processErr, interpolateErr := errors.New("process"), errors.New("interpolate")
	runErr, extractErr := errors.New("run"), errors.New("extract")

	testCases := []struct {
		name       string
		setupMocks func(*MockCommandRunner, *MockFileProcessor, *MockInterpolator, *MockOutputExtractor)
	}{
		{
			name: "File Process Error",
			setupMocks: func(r *MockCommandRunner, fp *MockFileProcessor, i *MockInterpolator, oe *MockOutputExtractor) {
				fp.On("Process", mock.Anything, mock.Anything).Return(processErr).Once()
			},
		},
		{
			name: "Interpolate Error",
			setupMocks: func(r *MockCommandRunner, fp *MockFileProcessor, i *MockInterpolator, oe *MockOutputExtractor) {
				fp.On("Process", mock.Anything, mock.Anything).Return(nil).Once()
				i.On("Interpolate", mock.Anything, mock.Anything).Return("", interpolateErr).Once()
				fp.On("Restore").Return(nil).Once()
			},
		},
		{
			name: "Runner Error",
			setupMocks: func(r *MockCommandRunner, fp *MockFileProcessor, i *MockInterpolator, oe *MockOutputExtractor) {
				fp.On("Process", mock.Anything, mock.Anything).Return(nil).Once()
				i.On("Interpolate", mock.Anything, mock.Anything).Return("cmd", nil).Once()
				r.On("Run", mock.Anything, "cmd", mock.Anything).Return(nil, runErr).Once()
				fp.On("Restore").Return(nil).Once()
			},
		},
		{
			name: "Command Non-Zero Exit",
			setupMocks: func(r *MockCommandRunner, fp *MockFileProcessor, i *MockInterpolator, oe *MockOutputExtractor) {
				fp.On("Process", mock.Anything, mock.Anything).Return(nil).Once()
				i.On("Interpolate", mock.Anything, mock.Anything).Return("cmd", nil).Once()
				r.On("Run", mock.Anything, "cmd", mock.Anything).Return(&vos.CommandResult{ExitCode: 1}, nil).Once()
				fp.On("Restore").Return(nil).Once()
			},
		},
		{
			name: "Extractor Error",
			setupMocks: func(r *MockCommandRunner, fp *MockFileProcessor, i *MockInterpolator, oe *MockOutputExtractor) {
				fp.On("Process", mock.Anything, mock.Anything).Return(nil).Once()
				i.On("Interpolate", mock.Anything, mock.Anything).Return("cmd", nil).Once()
				r.On("Run", mock.Anything, "cmd", mock.Anything).Return(&vos.CommandResult{ExitCode: 0}, nil).Once()
				oe.On("ExtractVars", mock.Anything, mock.Anything).Return(nil, extractErr).Once()
				fp.On("Restore").Return(nil).Once()
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			runner, fileProcessor := new(MockCommandRunner), new(MockFileProcessor)
			interpolator, outputExtractor := new(MockInterpolator), new(MockOutputExtractor)

			executor := services.NewCommandExecutor(runner, fileProcessor, interpolator, outputExtractor)
			cmd, _ := vos.NewCommand("name_cmd", "my command")

			tc.setupMocks(runner, fileProcessor, interpolator, outputExtractor)

			result := executor.Execute(context.Background(), cmd, vos.VariableSet{}, "/app", "/app")

			require.NotNil(t, result.Error)
			assert.Equal(t, vos.Failure, result.Status)
			mock.AssertExpectationsForObjects(t, runner, fileProcessor, interpolator, outputExtractor)
		})
	}
}
