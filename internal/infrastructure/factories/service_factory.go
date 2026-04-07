package factories

import (
	"os"
	"path/filepath"

	app "github.com/jairoprogramador/vex/internal/application"
	docSer "github.com/jairoprogramador/vex/internal/domain/docker/services"
	proPor "github.com/jairoprogramador/vex/internal/domain/project/ports"
	"github.com/jairoprogramador/vex/internal/infrastructure/architecture"
	"github.com/jairoprogramador/vex/internal/infrastructure/common"
	"github.com/jairoprogramador/vex/internal/infrastructure/docker"
	"github.com/jairoprogramador/vex/internal/infrastructure/project"
)

type ServiceFactory interface {
	BuildExecutor() (*app.ExecutorService, error)
	BuildInitialize() (*app.InitializeService, error)
	BuildArchitecture() (*app.ArchitectureService, error)
}

type serviceFactory struct{}

func NewServiceFactory() ServiceFactory {
	return &serviceFactory{}
}

func (f *serviceFactory) BuildInitialize() (*app.InitializeService, error) {
	projectPath, err := f.getProjectPath()
	if err != nil {
		return nil, err
	}
	projectRepository, err := f.getProjectRepository(projectPath)
	if err != nil {
		return nil, err
	}

	inputService := common.NewSurveyUserInputService()
	versionService := project.NewHttpVersion()
	levelRepository := architecture.NewCacheLevelRepository()
	questionRepository := architecture.NewCacheQuestionRepository()
	templateRepository := architecture.NewCacheTemplateRepository(
		f.templateCachePath(), f.templateRemoteURL())
	return app.NewInitializeService(
		filepath.Base(projectPath), projectRepository, inputService,
		versionService, levelRepository, questionRepository, templateRepository), nil
}

func (f *serviceFactory) BuildExecutor() (*app.ExecutorService, error) {
	projectPath, err := f.getProjectPath()
	if err != nil {
		return nil, err
	}
	projectRepository, err := f.getProjectRepository(projectPath)
	if err != nil {
		return nil, err
	}

	cmdExecutor := docker.NewShellExecutor()
	imageService := docSer.NewImageBuilder()
	containerService := docSer.NewContainerBuilder()

	return app.NewExecutorService(
		projectRepository, cmdExecutor, imageService, containerService), nil
}

func (f *serviceFactory) BuildArchitecture() (*app.ArchitectureService, error) {
	projectPath, err := f.getProjectPath()
	if err != nil {
		return nil, err
	}
	projectRepository, err := f.getProjectRepository(projectPath)
	if err != nil {
		return nil, err
	}

	questionRepository := architecture.NewCacheQuestionRepository()
	levelRepository := architecture.NewCacheLevelRepository()
	templateRepository := architecture.NewCacheTemplateRepository(
		f.templateCachePath(), f.templateRemoteURL())
	inputService := common.NewSurveyUserInputService()
	return app.NewArchitectureService(
		questionRepository, levelRepository,
		templateRepository, projectRepository, inputService), nil
}

func (f *serviceFactory) getProjectRepository(projectPath string) (proPor.ProjectRepository, error) {
	projectRepository := project.NewYAMLProjectRepository(projectPath)
	return projectRepository, nil
}

func (f *serviceFactory) getProjectPath() (string, error) {
	return os.Getwd()
}

func (f *serviceFactory) templateCachePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".vex", "templates.json")
}

func (f *serviceFactory) templateRemoteURL() string {
	if url := os.Getenv("VEX_STORE_TEMPLATE"); url != "" {
		return url
	}
	return "https://raw.githubusercontent.com/jairoprogramador/vex-template-store/main/templates.json"
}
