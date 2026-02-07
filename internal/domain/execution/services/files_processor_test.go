package services_test

import (
	"errors"
	"testing"

	"github.com/jairoprogramador/vex/internal/domain/execution/ports"
	"github.com/jairoprogramador/vex/internal/domain/execution/services"
	"github.com/jairoprogramador/vex/internal/domain/execution/vos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockFileSystem struct {
	mock.Mock
}

var _ ports.FileSystem = (*MockFileSystem)(nil)

func (m *MockFileSystem) ReadFile(path string) ([]byte, error) {
	args := m.Called(path)
	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockFileSystem) WriteFile(path string, data []byte) error {
	args := m.Called(path, data)
	return args.Error(0)
}

type MockInterpolatorFilesProcessor struct {
	mock.Mock
}

var _ ports.Interpolator = (*MockInterpolatorFilesProcessor)(nil)

func (m *MockInterpolatorFilesProcessor) Interpolate(input string, vars vos.VariableSet) (string, error) {
	args := m.Called(input, vars)
	return args.String(0), args.Error(1)
}

func newOutputVarForTestFilesProcessor(name, value string, isShared bool) vos.OutputVar {
	v, _ := vos.NewOutputVar(name, value, isShared)
	return v
}

func TestFileProcessor_Process(t *testing.T) {
	fs := new(MockFileSystem)
	interpolator := new(MockInterpolatorFilesProcessor)
	processor := services.NewFileProcessor(fs, interpolator)

	vars := vos.NewVariableSet()
	vars.Add(newOutputVarForTestFilesProcessor("name", "World", false))
	filePath := "/tmp/test.txt"
	originalContent := "Hello, ${var.name}!"
	interpolatedContent := "Hello, World!"

	// Configurar mocks para el caso exitoso
	fs.On("ReadFile", filePath).Return([]byte(originalContent), nil).Once()
	interpolator.On("Interpolate", originalContent, vars).Return(interpolatedContent, nil).Once()
	fs.On("WriteFile", filePath, []byte(interpolatedContent)).Return(nil).Once()

	err := processor.Process([]string{filePath}, vars)
	require.NoError(t, err)

	// Verificar que los mocks fueron llamados como se esperaba
	fs.AssertExpectations(t)
	interpolator.AssertExpectations(t)
}

func TestFileProcessor_Process_Idempotency(t *testing.T) {
	fs := new(MockFileSystem)
	interpolator := new(MockInterpolatorFilesProcessor)
	processor := services.NewFileProcessor(fs, interpolator)

	vars := vos.NewVariableSet()
	vars.Add(newOutputVarForTestFilesProcessor("name", "World", false))
	filePath := "/tmp/test.txt"
	originalContent := "Hello, ${var.name}!"
	interpolatedContent := "Hello, World!"

	// ReadFile solo debe ser llamado una vez
	fs.On("ReadFile", filePath).Return([]byte(originalContent), nil).Once()
	// Interpolate y WriteFile deben ser llamados dos veces
	interpolator.On("Interpolate", originalContent, vars).Return(interpolatedContent, nil).Twice()
	fs.On("WriteFile", filePath, []byte(interpolatedContent)).Return(nil).Twice()

	// Primera llamada
	err1 := processor.Process([]string{filePath}, vars)
	require.NoError(t, err1)

	// Segunda llamada
	err2 := processor.Process([]string{filePath}, vars)
	require.NoError(t, err2)

	fs.AssertExpectations(t)
	interpolator.AssertExpectations(t)
}

func TestFileProcessor_Restore(t *testing.T) {
	fs := new(MockFileSystem)
	interpolator := new(MockInterpolatorFilesProcessor)
	processor := services.NewFileProcessor(fs, interpolator)

	vars := vos.NewVariableSet()
	vars.Add(newOutputVarForTestFilesProcessor("name", "World", false))
	filePath := "/tmp/test.txt"
	originalContent := "Hello, ${var.name}!"
	interpolatedContent := "Hello, World!"

	// Setup para Process
	fs.On("ReadFile", filePath).Return([]byte(originalContent), nil).Once()
	interpolator.On("Interpolate", originalContent, vars).Return(interpolatedContent, nil).Once()
	fs.On("WriteFile", filePath, []byte(interpolatedContent)).Return(nil).Once()

	// Ejecutar Process para que haya algo que restaurar
	err := processor.Process([]string{filePath}, vars)
	require.NoError(t, err)

	// Setup para Restore
	fs.On("WriteFile", filePath, []byte(originalContent)).Return(nil).Once()

	err = processor.Restore()
	require.NoError(t, err)

	fs.AssertExpectations(t)
	interpolator.AssertExpectations(t)
}

func TestFileProcessor_Process_ReadFileError(t *testing.T) {
	fs := new(MockFileSystem)
	interpolator := new(MockInterpolatorFilesProcessor)
	processor := services.NewFileProcessor(fs, interpolator)

	filePath := "/tmp/test.txt"
	readErr := errors.New("read error")

	fs.On("ReadFile", filePath).Return([]byte{}, readErr).Once()

	err := processor.Process([]string{filePath}, vos.VariableSet{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), readErr.Error())

	fs.AssertExpectations(t)
}

func TestFileProcessor_Restore_WriteFileError(t *testing.T) {
	fs := new(MockFileSystem)
	interpolator := new(MockInterpolatorFilesProcessor)
	processor := services.NewFileProcessor(fs, interpolator)

	// Proceso un archivo primero
	filePath := "/tmp/file1.txt"
	originalContent := "original"
	fs.On("ReadFile", filePath).Return([]byte(originalContent), nil).Once()
	interpolator.On("Interpolate", originalContent, mock.Anything).Return("interpolated", nil).Once()
	fs.On("WriteFile", filePath, []byte("interpolated")).Return(nil).Once()
	_ = processor.Process([]string{filePath}, vos.VariableSet{})

	// Setup para el error en Restore
	writeErr := errors.New("write error")
	fs.On("WriteFile", filePath, []byte(originalContent)).Return(writeErr).Once()

	err := processor.Restore()
	require.Error(t, err)
	assert.Contains(t, err.Error(), writeErr.Error())

	fs.AssertExpectations(t)
}
