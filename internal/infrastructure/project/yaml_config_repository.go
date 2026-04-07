package project

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/jairoprogramador/vex/internal/domain/project/aggregates"
	"github.com/jairoprogramador/vex/internal/domain/project/ports"
	"github.com/jairoprogramador/vex/internal/infrastructure/project/dto"
	"github.com/jairoprogramador/vex/internal/infrastructure/project/mapper"
	"gopkg.in/yaml.v3"
)

const configFileName = "vexconfig.yaml"

type yamlProjectRepository struct {
	projectPath string
}

func NewYAMLProjectRepository(projectPath string) ports.ProjectRepository {
	return &yamlProjectRepository{projectPath: projectPath}
}

func (r *yamlProjectRepository) Save(project *aggregates.Project) error {
	fdConfig := mapper.ToDto(project)
	data, err := yaml.Marshal(fdConfig)
	if err != nil {
		return err
	}
	return os.WriteFile(r.fdconfigPath(), data, 0644)
}

func (r *yamlProjectRepository) Exists() (bool, error) {
	_, err := os.Stat(r.fdconfigPath())
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (r *yamlProjectRepository) Load() (*aggregates.Project, error) {
	data, err := os.ReadFile(r.fdconfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.New("project configuration file not found")
		}
		return nil, err
	}

	var fdConfig dto.FDConfigDTO
	if err := yaml.Unmarshal(data, &fdConfig); err != nil {
		return nil, err
	}

	return mapper.ToDomain(fdConfig)
}

func (r *yamlProjectRepository) fdconfigPath() string {
	return filepath.Join(r.projectPath, configFileName)
}
