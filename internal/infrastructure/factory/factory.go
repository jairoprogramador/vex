package factory

import (
	"fmt"
	"os"
	"path/filepath"

	applic "github.com/jairoprogramador/vex/internal/application"
	defServ "github.com/jairoprogramador/vex/internal/domain/definition/services"
	exeServ "github.com/jairoprogramador/vex/internal/domain/execution/services"
	staServ "github.com/jairoprogramador/vex/internal/domain/state/services"
	verServ "github.com/jairoprogramador/vex/internal/domain/versioning/services"
	worVos "github.com/jairoprogramador/vex/internal/domain/workspace/vos"
	iDefini "github.com/jairoprogramador/vex/internal/infrastructure/definition"
	iExecut "github.com/jairoprogramador/vex/internal/infrastructure/execution"
	iLgRep "github.com/jairoprogramador/vex/internal/infrastructure/logger/repository"
	iLgSer "github.com/jairoprogramador/vex/internal/infrastructure/logger/service"
	iProje "github.com/jairoprogramador/vex/internal/infrastructure/project"
	iState "github.com/jairoprogramador/vex/internal/infrastructure/state"
	iVersi "github.com/jairoprogramador/vex/internal/infrastructure/versioning"

	"github.com/spf13/viper"
)

type ServiceFactory interface {
	BuildExecutionOrchestrator() (*applic.ExecutionOrchestrator, error)
	BuildLogService() *applic.LoggerService
	PathAppProject() string
}

type Factory struct {
	pathAppProject string
	pathAppVex     string
}

func NewFactory() (ServiceFactory, error) {
	vexHome := getVexHome()

	workingDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("error al obtener el directorio de trabajo: %w", err)
	}

	return &Factory{
		pathAppVex:     vexHome,
		pathAppProject: workingDir,
	}, nil
}

func (f *Factory) PathAppProject() string {
	return f.pathAppProject
}

func (f *Factory) BuildLogService() *applic.LoggerService {
	consolePresenter := iLgSer.NewConsolePresenterService()
	loggerRepository := iLgRep.NewFileLoggerRepository("")
	configRepository := iProje.NewYAMLProjectRepository()

	return applic.NewLoggerService(loggerRepository, configRepository, consolePresenter)
}

func (f *Factory) BuildExecutionOrchestrator() (*applic.ExecutionOrchestrator, error) {
	// Infrastructure Layer
	commandRunner := iExecut.NewShellCommandRunner()
	fileSystem := iExecut.NewOSFileSystem()
	gitClonerTemplate := iProje.NewGitClonerTemplate()
	gitRepository := iVersi.NewGoGitRepository()
	definitionReader := iDefini.NewYamlDefinitionReader()
	projectRepository := iProje.NewYAMLProjectRepository()
	fingerprintService := iState.NewSha256FingerprintService()
	stateRepository := iState.NewGobStateRepository()
	copyWorkdir := iExecut.NewCopyWorkdir()
	varsRepository := iExecut.NewGobVarsRepository()

	// Domain & Application Services
	projectService := applic.NewProjectService(projectRepository)
	workspaceService := applic.NewWorkspaceService()
	versionCalculator := verServ.NewVersionCalculator(gitRepository)
	planBuilder := defServ.NewPlanBuilder(definitionReader)
	stateManager := staServ.NewStateManager(stateRepository)
	interpolator := exeServ.NewInterpolator()
	fileProcessor := exeServ.NewFileProcessor(fileSystem, interpolator)
	outputExtractor := exeServ.NewOutputExtractor()
	commandExecutor := exeServ.NewCommandExecutor(commandRunner, fileProcessor, interpolator, outputExtractor)
	variableResolver := exeServ.NewVariableResolver(interpolator)
	stepExecutor := exeServ.NewStepExecutor(commandExecutor, variableResolver)

	orchestrator := applic.NewExecutionOrchestrator(
		f.pathAppProject,
		f.pathAppVex,
		projectService,
		workspaceService,
		gitClonerTemplate,
		versionCalculator,
		planBuilder,
		fingerprintService,
		stateManager,
		stepExecutor,
		copyWorkdir,
		varsRepository,
		gitRepository,
	)
	return orchestrator, nil
}

func getVexHome() string {
	viper.SetEnvPrefix("VEX")
	viper.AutomaticEnv()

	userHomeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Println("Error al obtener el directorio home:", err)
		os.Exit(1)
	}

	defaultHome := filepath.Join(userHomeDir, worVos.DefaultRootDir)
	vexHome := viper.GetString("HOME")
	if vexHome == "" {
		vexHome = defaultHome
	}
	return vexHome
}
