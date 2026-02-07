package application_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jairoprogramador/vex/internal/application"
	"github.com/jairoprogramador/vex/internal/domain/project/ports"
	"github.com/jairoprogramador/vex/internal/domain/project/vos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeProjectRepository es un mock para el ProjectRepository.
type fakeProjectRepository struct {
	LoadFunc func(ctx context.Context, path string) (*ports.ProjectConfigDTO, error)
	SaveFunc func(ctx context.Context, path string, data *ports.ProjectConfigDTO) error

	saveCalled bool
}

func (f *fakeProjectRepository) Load(ctx context.Context, path string) (*ports.ProjectConfigDTO, error) {
	if f.LoadFunc != nil {
		return f.LoadFunc(ctx, path)
	}
	return nil, errors.New("LoadFunc no implementado")
}

func (f *fakeProjectRepository) Save(ctx context.Context, path string, data *ports.ProjectConfigDTO) error {
	f.saveCalled = true
	if f.SaveFunc != nil {
		return f.SaveFunc(ctx, path, data)
	}
	return nil
}

func newValidMockDTO(modifiers ...func(*ports.ProjectConfigDTO)) *ports.ProjectConfigDTO {
	// 1. Define los datos base y consistentes
	projectName := "test-project"
	projectOrganization := "vex"
	projectTeam := "shikigami"
	expectedID := vos.GenerateProjectID(projectName, projectOrganization, projectTeam)

	// 2. Crea el DTO base
	dto := &ports.ProjectConfigDTO{
		ID:           expectedID.String(),
		Name:         projectName,
		Organization: projectOrganization,
		Team:         projectTeam,
		Version:      "1.0.0",
		TemplateURL:  "https://github.com/jairo/template.git",
		TemplateRef:  "v1",
	}

	// 3. Aplica cualquier modificación específica del test
	for _, modifier := range modifiers {
		modifier(dto)
	}

	return dto
}

func TestProjectService_Load_Success(t *testing.T) {
	// --- Arrange ---
	ctx := context.Background()

	mockRepo := &fakeProjectRepository{
		LoadFunc: func(ctx context.Context, path string) (*ports.ProjectConfigDTO, error) {
			return newValidMockDTO(), nil // Usamos el helper sin modificaciones
		},
	}
	service := application.NewProjectService(mockRepo)

	// --- Act ---
	project, err := service.Load(ctx, "/fake/path")

	// --- Assert ---
	require.NoError(t, err)
	require.NotNil(t, project)
	assert.Equal(t, "test-project", project.Data().Name())
	assert.False(t, mockRepo.saveCalled, "Save no debería haber sido llamado porque el ID no cambió")
}

func TestProjectService_Load_IDChangesAndSaves(t *testing.T) {
	// --- Arrange ---
	ctx := context.Background()

	mockRepo := &fakeProjectRepository{
		LoadFunc: func(ctx context.Context, path string) (*ports.ProjectConfigDTO, error) {
			// Usamos el helper y le pasamos una función para modificar solo el ID
			return newValidMockDTO(func(dto *ports.ProjectConfigDTO) {
				dto.ID = "id-incorrecto"
			}), nil
		},
	}
	service := application.NewProjectService(mockRepo)

	// --- Act ---
	project, err := service.Load(ctx, "/fake/path")

	// --- Assert ---
	require.NoError(t, err)
	require.NotNil(t, project)

	// Verificamos contra el ID que el helper habría calculado
	validDTO := newValidMockDTO()
	assert.Equal(t, validDTO.ID, project.ID().String(), "El ID del proyecto debería haberse actualizado")
	assert.True(t, mockRepo.saveCalled, "Save debería haber sido llamado porque el ID cambió")
}

func TestProjectService_Load_RepoLoadFails(t *testing.T) {
	// --- Arrange ---
	ctx := context.Background()
	expectedError := errors.New("failed to read file")

	mockRepo := &fakeProjectRepository{
		LoadFunc: func(ctx context.Context, path string) (*ports.ProjectConfigDTO, error) {
			return nil, expectedError
		},
	}
	service := application.NewProjectService(mockRepo)

	// --- Act ---
	project, err := service.Load(ctx, "/fake/path")

	// --- Assert ---
	require.Error(t, err)
	assert.Nil(t, project)
	assert.Contains(t, err.Error(), expectedError.Error())
}

func TestProjectService_Load_SaveFailsAfterIDChange(t *testing.T) {
	// --- Arrange ---
	ctx := context.Background()
	expectedError := errors.New("permission denied")

	mockRepo := &fakeProjectRepository{
		LoadFunc: func(ctx context.Context, path string) (*ports.ProjectConfigDTO, error) {
			return newValidMockDTO(func(dto *ports.ProjectConfigDTO) {
				dto.ID = "id-incorrecto"
			}), nil
		},
		SaveFunc: func(ctx context.Context, path string, data *ports.ProjectConfigDTO) error {
			return expectedError
		},
	}
	service := application.NewProjectService(mockRepo)

	// --- Act ---
	project, err := service.Load(ctx, "/fake/path")

	// --- Assert ---
	require.Error(t, err)
	assert.Nil(t, project)
	assert.True(t, mockRepo.saveCalled)
	assert.Contains(t, err.Error(), expectedError.Error())
}
