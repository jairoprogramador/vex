package application

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/jairoprogramador/vex/internal/domain/project/aggregates"
	"github.com/jairoprogramador/vex/internal/domain/project/ports"
	"github.com/jairoprogramador/vex/internal/domain/project/vos"
)

type ProjectService struct {
	projectRepo ports.ProjectRepository
}

func NewProjectService(
	projectRepo ports.ProjectRepository) *ProjectService {

	return &ProjectService{
		projectRepo: projectRepo,
	}
}

func (s *ProjectService) Load(
	ctx context.Context, projectLocalPath string) (*aggregates.Project, error) {
	projectConfigPath := filepath.Join(projectLocalPath, "vexconfig.yaml")

	projectDTO, err := s.projectRepo.Load(ctx, projectConfigPath)
	if err != nil {
		return nil, fmt.Errorf("no se pudo cargar la configuración del proyecto: %w", err)
	}

	projectData, err := vos.NewProjectData(
		projectDTO.Name, projectDTO.Organization,
		projectDTO.Team, projectDTO.Description, projectDTO.Version)

	if err != nil {
		return nil, fmt.Errorf("datos del proyecto inválidos: %w", err)
	}
	templateRepo, err := vos.NewTemplateRepository(projectDTO.TemplateURL, projectDTO.TemplateRef)
	if err != nil {
		return nil, fmt.Errorf("datos del repositorio de plantillas inválidos: %w", err)
	}
	projectID := vos.NewProjectID(projectDTO.ID)

	project := aggregates.NewProject(projectID, projectData, templateRepo, projectLocalPath)

	if project.SyncID() {
		fmt.Println("El ID del proyecto ha cambiado. Actualizando vexconfig.yaml...")
		projectDTO.ID = project.ID().String()
		if err := s.projectRepo.Save(ctx, projectConfigPath, projectDTO); err != nil {
			return nil, fmt.Errorf("no se pudo guardar el ID del proyecto actualizado: %w", err)
		}
	}

	return project, nil
}
