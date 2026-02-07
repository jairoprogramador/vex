package application

import (
	"github.com/jairoprogramador/vex/internal/domain/workspace/aggregates"
	"github.com/jairoprogramador/vex/internal/domain/workspace/vos"
)

// WorkspaceService es responsable de crear y gestionar el agregado Workspace.
type WorkspaceService struct{}

// NewWorkspaceService crea una nueva instancia de WorkspaceService.
func NewWorkspaceService() *WorkspaceService {
	return &WorkspaceService{}
}

// Load crea una instancia del agregado Workspace a partir de los datos proporcionados.
func (s *WorkspaceService) NewWorkspace(rootVexPath, projectName, templateName string) (*aggregates.Workspace, error) {
	wsRootPath, err := vos.NewRootPath(rootVexPath)
	if err != nil {
		return nil, err
	}

	wsProjectName, err := vos.NewProjectName(projectName)
	if err != nil {
		return nil, err
	}

	wsTemplateName, err := vos.NewTemplateName(templateName)
	if err != nil {
		return nil, err
	}

	return aggregates.NewWorkspace(wsRootPath, wsProjectName, wsTemplateName)
}
