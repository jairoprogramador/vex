package application

import (
	"context"
	"errors"
	"fmt"

	arqAgg "github.com/jairoprogramador/vex-client/internal/domain/architecture/aggregates"
	arqPor "github.com/jairoprogramador/vex-client/internal/domain/architecture/ports"
	arqVos "github.com/jairoprogramador/vex-client/internal/domain/architecture/vos"

	comPor "github.com/jairoprogramador/vex-client/internal/domain/common/ports"
	proAgg "github.com/jairoprogramador/vex-client/internal/domain/project/aggregates"
	proPor "github.com/jairoprogramador/vex-client/internal/domain/project/ports"
	proVos "github.com/jairoprogramador/vex-client/internal/domain/project/vos"
)

const MessageProjectAlreadyExists = "project already initialized, vexconfig.yaml exists"

type InitializeService struct {
	projectRepository  proPor.ProjectRepository
	inputService       comPor.UserInputService
	versionService     proPor.Version
	levelRepository    arqPor.LevelRepository
	questionRepository arqPor.QuestionRepository
	templateRepository arqPor.TemplateRepository
	projectName        string
}

func NewInitializeService(
	projectName string,
	repository proPor.ProjectRepository,
	inputSvc comPor.UserInputService,
	versionSvc proPor.Version,
	levelRepository arqPor.LevelRepository,
	questionRepository arqPor.QuestionRepository,
	templateRepository arqPor.TemplateRepository,
) *InitializeService {
	return &InitializeService{
		projectRepository:  repository,
		inputService:       inputSvc,
		versionService:     versionSvc,
		levelRepository:    levelRepository,
		questionRepository: questionRepository,
		templateRepository: templateRepository,
		projectName:        projectName,
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

	templateStrURL, err := s.getTemplate()
	if err != nil {
		fmt.Println("Warning: Error getting template:", err)
		templateStrURL = proVos.DefaultTemplateUrl
	}

	var project *proAgg.Project
	if interactive {
		project, err = s.createProjectFromUserInput(templateStrURL)
		if err != nil {
			return err
		}
	} else {
		project, err = s.createDefaultProject(templateStrURL)
		if err != nil {
			return err
		}
	}

	return s.projectRepository.Save(project)
}

func (s *InitializeService) createProjectFromUserInput(templateStrURL string) (*proAgg.Project, error) {
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
	templateURL, err := s.inputService.Ask("Template URL", templateStrURL)
	if err != nil {
		return nil, err
	}
	templateRef, err := s.inputService.Ask("Template Ref", proVos.DefaultTemplateRef)
	if err != nil {
		return nil, err
	}
	containerImage, err := s.inputService.Ask("Container Image", proVos.DefaultContainerImage)
	if err != nil {
		return nil, err
	}
	containerTag, err := s.inputService.Ask("Container Image Tag", proVos.DefaultContainerTag)
	if err != nil {
		return nil, err
	}

	projectData, err := proVos.NewProjectData(name, org, team, "")
	if err != nil {
		return nil, err
	}

	template, err := proVos.NewTemplate(templateURL, templateRef)
	if err != nil {
		return nil, err
	}

	container, err := proVos.NewImage(containerImage, containerTag)
	if err != nil {
		return nil, err
	}

	runtime := proVos.NewRuntime(container, []proVos.Volume{}, []proVos.EnvVar{}, []proVos.Argument{})

	projectID, err := s.getProjectID(projectData)
	if err != nil {
		return nil, err
	}

	return proAgg.NewProject(projectID, projectData, template, runtime)
}

func (s *InitializeService) createDefaultProject(templateStrURL string) (*proAgg.Project, error) {
	projectData, err := proVos.NewProjectData(
		s.projectName, proVos.DefaultProjectOrganization, proVos.DefaultProjectTeam, "")
	if err != nil {
		return nil, err
	}

	template, err := proVos.NewTemplate(templateStrURL, proVos.DefaultTemplateRef)
	if err != nil {
		return nil, err
	}

	container, err := proVos.NewImage(proVos.DefaultContainerImage, proVos.DefaultContainerTag)
	if err != nil {
		return nil, err
	}

	runtime := proVos.NewRuntime(container, []proVos.Volume{}, []proVos.EnvVar{}, []proVos.Argument{})

	projectID, err := s.getProjectID(projectData)
	if err != nil {
		return nil, err
	}

	return proAgg.NewProject(projectID, projectData, template, runtime)
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

func (s *InitializeService) getTemplate() (string, error) {
	questions, err := s.questionRepository.GetQuestions()
	if err != nil {
		return "", err
	}

	levels, err := s.levelRepository.GetLevels()
	if err != nil {
		return "", err
	}
	levelMin, err := s.getLevelMin(levels)
	if err != nil {
		return "", err
	}
	responsesMin, err := s.getResponsesMin(questions)
	if err != nil {
		return "", err
	}
	template, err := s.templateRepository.GetTemplates(levelMin, responsesMin)
	if err != nil {
		return "", err
	}
	return template, nil
}
