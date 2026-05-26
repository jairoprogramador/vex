package application

import (
	"context"
	"errors"
	"fmt"

	arqAgg "github.com/jairoprogramador/vex/internal/domain/architecture/aggregates"
	arqPor "github.com/jairoprogramador/vex/internal/domain/architecture/ports"
	arqVos "github.com/jairoprogramador/vex/internal/domain/architecture/vos"

	comPor "github.com/jairoprogramador/vex/internal/domain/common/ports"
	comVos "github.com/jairoprogramador/vex/internal/domain/common/vos"

	proAgg "github.com/jairoprogramador/vex/internal/domain/project/aggregates"
	proPor "github.com/jairoprogramador/vex/internal/domain/project/ports"
	proVos "github.com/jairoprogramador/vex/internal/domain/project/vos"
)

const MessageProjectAlreadyExists = "project already initialized, vexconfig.yaml exists"

type InitializeService struct {
	projectRepository  proPor.ProjectRepository
	inputService       comPor.UserInputService
	versionService     proPor.Version
	levelRepository    arqPor.LevelRepository
	questionRepository arqPor.QuestionRepository
	templateRepository arqPor.TemplateRepository
	gitInfo            proPor.GitInfo
	projectPath        string
	projectName        string
}

func NewInitializeService(
	projectName string,
	projectPath string,
	repository proPor.ProjectRepository,
	inputSvc comPor.UserInputService,
	versionSvc proPor.Version,
	levelRepository arqPor.LevelRepository,
	questionRepository arqPor.QuestionRepository,
	templateRepository arqPor.TemplateRepository,
	gitInfo proPor.GitInfo,
) *InitializeService {
	return &InitializeService{
		projectRepository:  repository,
		inputService:       inputSvc,
		versionService:     versionSvc,
		levelRepository:    levelRepository,
		questionRepository: questionRepository,
		templateRepository: templateRepository,
		gitInfo:            gitInfo,
		projectName:        projectName,
		projectPath:        projectPath,
	}
}

func (s *InitializeService) Run(ctx context.Context, interactive bool) error {
	exists, err := s.projectRepository.Exists()
	if err != nil {
		return err
	}
	if exists {
		project, err := s.projectRepository.Load()
		if err != nil {
			return err
		}
		if project.IsIDDirty() {
			return s.projectRepository.Save(project)
		}
		return errors.New(MessageProjectAlreadyExists)
	}

	executionUnit, err := s.GetExecutionUnit()
	if err != nil {
		fmt.Println("Warning: Error getting pipeline:", err)
		image, _ := comVos.NewImage("")
		pipeline, _ := comVos.NewPipeline(comVos.DefaultPipelineUrl, comVos.DefaultPipelineRef)
		executionUnit = arqVos.NewExecutionUnit(image, pipeline)
	}

	url, ref := s.detectGitInfo(ctx)

	var project *proAgg.Project
	if interactive {
		project, err = s.createProjectFromUserInput(executionUnit, url, ref)
		if err != nil {
			return err
		}
	} else {
		project, err = s.createDefaultProject(executionUnit, url, ref)
		if err != nil {
			return err
		}
	}

	return s.projectRepository.Save(project)
}

func (s *InitializeService) detectGitInfo(ctx context.Context) (url, ref string) {
	url, err := s.gitInfo.RemoteURL(ctx, s.projectPath)
	if err != nil {
		fmt.Println("Warning: could not detect git remote 'origin':", err)
		url = ""
	}

	ref, err = s.gitInfo.CurrentRef(ctx, s.projectPath)
	if err != nil {
		fmt.Println("Warning: could not detect current git branch:", err)
		ref = ""
	}
	return url, ref
}

func (s *InitializeService) createProjectFromUserInput(executionUnit arqVos.ExecutionUnit, defaultURL, defaultRef string) (*proAgg.Project, error) {
	name, err := s.inputService.Ask("Project Name", s.projectName)
	if err != nil {
		return nil, err
	}
	team, err := s.inputService.Ask("Project Team", proVos.DefaultProjectTeam)
	if err != nil {
		return nil, err
	}
	org, err := s.inputService.Ask("Project Organization", proVos.DefaultProjectOrganization)
	if err != nil {
		return nil, err
	}
	url, err := s.inputService.Ask("Project Repository URL", defaultURL)
	if err != nil {
		return nil, err
	}
	ref, err := s.inputService.Ask("Project Repository Ref", defaultRef)
	if err != nil {
		return nil, err
	}

	projectData, err := proVos.NewProjectData(name, org, team, "", url, ref)
	if err != nil {
		return nil, err
	}

	runtime := proVos.NewRuntime(proVos.WithImage(executionUnit.Image()))

	projectID, err := s.getProjectID(projectData)
	if err != nil {
		return nil, err
	}

	return proAgg.NewProject(projectID, projectData, executionUnit.Template(), runtime)
}

func (s *InitializeService) createDefaultProject(executionUnit arqVos.ExecutionUnit, url, ref string) (*proAgg.Project, error) {
	projectData, err := proVos.NewProjectData(
		s.projectName, proVos.DefaultProjectOrganization, proVos.DefaultProjectTeam, "", url, ref)
	if err != nil {
		return nil, err
	}
	runtime := proVos.NewRuntime(proVos.WithImage(executionUnit.Image()))

	projectID, err := s.getProjectID(projectData)
	if err != nil {
		return nil, err
	}

	return proAgg.NewProject(projectID, projectData, executionUnit.Template(), runtime)
}

func (s *InitializeService) getProjectID(data proVos.ProjectData) (proVos.ProjectID, error) {
	generatedID := proVos.GenerateProjectID(data.Name(), data.Organization(), data.Team())
	return proVos.NewProjectID(generatedID.String())
}

func (s *InitializeService) getLevelMin(levels []arqVos.Level) (arqVos.Level, error) {
	if len(levels) == 0 {
		return arqVos.Level{}, errors.New("levels are required")
	}
	levelMin := levels[0]
	for _, level := range levels {
		if level.Value() < levelMin.Value() {
			levelMin = level
		}
	}
	return levelMin, nil
}

func (s *InitializeService) getResponsesMin(questions []*arqAgg.Question) ([]int, error) {
	if len(questions) == 0 {
		return nil, errors.New("questions are required")
	}
	responses := make([]int, len(questions))
	for index, question := range questions {
		min := question.Points()[0].Value()
		for _, point := range question.Points() {
			if point.Value() < min {
				min = point.Value()
			}
		}
		responses[index] = min
	}
	return responses, nil
}

func (s *InitializeService) GetExecutionUnit() (arqVos.ExecutionUnit, error) {
	questions, err := s.questionRepository.GetQuestions()
	if err != nil {
		return arqVos.ExecutionUnit{}, err
	}

	levels, err := s.levelRepository.GetLevels()
	if err != nil {
		return arqVos.ExecutionUnit{}, err
	}
	levelMin, err := s.getLevelMin(levels)
	if err != nil {
		return arqVos.ExecutionUnit{}, err
	}
	responsesMin, err := s.getResponsesMin(questions)
	if err != nil {
		return arqVos.ExecutionUnit{}, err
	}

	query := arqVos.NewQueryTemplate(
		arqVos.WithStack(proVos.DefaultStack),
		arqVos.WithPlatform(proVos.DefaultPlatform),
		arqVos.WithLevel(levelMin.Value()),
		arqVos.WithCost(responsesMin[len(responsesMin)-1]),
	)

	executionUnit, err := s.templateRepository.GetExecutionUnit(query)
	if err != nil {
		return arqVos.ExecutionUnit{}, err
	}
	return executionUnit, nil
}
