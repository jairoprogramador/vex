package application

import (
	"errors"

	"github.com/jairoprogramador/vex-client/internal/domain/architecture/ports"
	arqSer "github.com/jairoprogramador/vex-client/internal/domain/architecture/services"
	arqVos "github.com/jairoprogramador/vex-client/internal/domain/architecture/vos"

	comPor "github.com/jairoprogramador/vex-client/internal/domain/common/ports"

	proPor "github.com/jairoprogramador/vex-client/internal/domain/project/ports"
	proVos "github.com/jairoprogramador/vex-client/internal/domain/project/vos"
)

type ArchitectureService struct {
	questionRepository ports.QuestionRepository
	levelRepository    ports.LevelRepository
	templateRepository ports.TemplateRepository
	projectRepository  proPor.ProjectRepository
	inputService       comPor.UserInputService
}

func NewArchitectureService(
	questionRepository ports.QuestionRepository,
	levelRepository ports.LevelRepository,
	templateRepository ports.TemplateRepository,
	projectRepository proPor.ProjectRepository,
	inputService comPor.UserInputService) *ArchitectureService {

	return &ArchitectureService{
		questionRepository: questionRepository,
		levelRepository:    levelRepository,
		templateRepository: templateRepository,
		projectRepository:  projectRepository,
		inputService:       inputService,
	}
}

func (s *ArchitectureService) Run() error {
	exists, err := s.projectRepository.Exists()
	if err != nil {
		return err
	}
	if !exists {
		return errors.New(MessageProjectNotInitialized)
	}

	project, err := s.projectRepository.Load()
	if err != nil {
		return err
	}

	questions, err := s.questionRepository.GetQuestions()
	if err != nil {
		return err
	}

	levels, err := s.levelRepository.GetLevels()
	if err != nil {
		return err
	}

	responses := make([]int, len(questions))
	for index, question := range questions {
		options := make([]string, len(question.Points()))
		for j, point := range question.Points() {
			options[j] = point.Message()
		}
		descriptions := make([]string, len(question.Points()))
		for j, point := range question.Points() {
			descriptions[j] = point.Description()
		}
		indexPoint, err := s.inputService.AskSelect(question.Ask(), options, descriptions)
		if err != nil {
			return err
		}
		responses[index] = question.Points()[indexPoint].Value()
	}

	score, err := arqSer.Score(responses, questions)
	if err != nil {
		return err
	}

	level, err := arqSer.Level(score, levels, questions)
	if err != nil {
		return err
	}

	query := arqVos.NewQueryTemplate(
		arqVos.WithStack(proVos.DefaultStack),
		arqVos.WithPlatform(proVos.DefaultPlatform),
		arqVos.WithLevel(level.Value()),
		arqVos.WithCost(responses[len(responses)-1]),
	)

	executionUnit, err := s.templateRepository.GetExecutionUnit(query)
	if err != nil {
		return err
	}
	runtime := proVos.NewRuntime(
		proVos.WithImage(executionUnit.Image()),
		proVos.WithVolumes(project.Runtime().Volumes()),
		proVos.WithEnv(project.Runtime().Env()),
		proVos.WithArgs(project.Runtime().Args()),
	)

	project.SetTemplate(executionUnit.Template())
	project.SetRuntime(runtime)

	err = s.projectRepository.Save(project)
	if err != nil {
		return err
	}

	return nil
}
