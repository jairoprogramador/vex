package application

import (
	"context"

	appDto "github.com/jairoprogramador/vex/internal/application/dto"
	appPor "github.com/jairoprogramador/vex/internal/application/ports"

	proPor "github.com/jairoprogramador/vex/internal/domain/project/ports"

	"github.com/jairoprogramador/vex/internal/domain/logger/aggregates"
	"github.com/jairoprogramador/vex/internal/domain/logger/entities"
	"github.com/jairoprogramador/vex/internal/domain/logger/ports"
)

type LoggerService struct {
	loggerRepository ports.LoggerRepository
	configRepository proPor.ProjectRepository
	presenter        appPor.PresenterService
}

func NewLoggerService(
	loggerRepository ports.LoggerRepository,
	configRepository proPor.ProjectRepository,
	presenter appPor.PresenterService) *LoggerService {
	return &LoggerService{
		loggerRepository: loggerRepository,
		configRepository: configRepository,
		presenter:        presenter,
	}
}

func (l *LoggerService) ShowLog(pathProject string) error {

	configProject, err := l.configRepository.Load(context.Background(), pathProject)
	if err != nil {
		return err
	}

	namesParams := appDto.NewNamesParams(configProject.Name, configProject.TemplateURL)

	logger, err := l.loggerRepository.Find(namesParams)
	if err != nil {
		return err
	}

	l.presenter.Header(&logger, logger.Revision())
	for _, step := range logger.Steps() {
		l.presenter.Step(step)
		for _, task := range step.Tasks() {
			l.presenter.Task(task, step)
		}
	}
	l.presenter.FinalSummary(&logger)

	return nil
}

func (l *LoggerService) ShowError(itemName string, itemErr error) error {
	logger := aggregates.NewLogger(nil, "")
	logger.Start()

	stepLog, err := entities.NewStepRecord(itemName)
	if err != nil {
		return err
	}

	if err := logger.AddStep(stepLog); err != nil {
		return err
	}

	stepLog.MarkAsRunning()
	stepLog.MarkAsFailure(itemErr)
	if l.presenter != nil {
		l.presenter.Line()
		l.presenter.Step(stepLog)
		l.presenter.FinalSummary(logger)
	}
	return nil
}

func (l *LoggerService) StartLog(namesParams appDto.NamesParams, contextData map[string]string, revision string) (*aggregates.Logger, error) {
	log := aggregates.NewLogger(contextData, revision)
	log.Start()

	if err := l.loggerRepository.Save(namesParams, log); err != nil {
		return nil, err
	}

	if l.presenter != nil {
		l.presenter.Header(log, revision)
	}
	return log, nil
}

func (l *LoggerService) AddStep(namesParams appDto.NamesParams, logger *aggregates.Logger, stepName string) (*entities.StepRecord, error) {
	step, err := entities.NewStepRecord(stepName)
	if err != nil {
		return nil, err
	}

	if err := logger.AddStep(step); err != nil {
		return nil, err
	}

	if err := l.loggerRepository.Save(namesParams, logger); err != nil {
		return nil, err
	}

	return step, nil
}

func (l *LoggerService) AddTaskToStep(namesParams appDto.NamesParams, logger *aggregates.Logger, stepName, taskName string) (*entities.TaskRecord, error) {
	step, err := logger.GetStep(stepName)
	if err != nil {
		return nil, err
	}

	task, err := entities.NewTaskRecord(taskName)
	if err != nil {
		return nil, err
	}

	step.AddTask(task)

	if err := l.loggerRepository.Save(namesParams, logger); err != nil {
		return nil, err
	}

	return task, nil
}

func (l *LoggerService) MarkStepAsSuccessful(namesParams appDto.NamesParams, logger *aggregates.Logger, step *entities.StepRecord) error {
	step.MarkAsSuccess()
	if l.presenter != nil {
		l.presenter.Step(step)
	}
	return l.loggerRepository.Save(namesParams, logger)
}

func (l *LoggerService) MarkStepAsFailed(namesParams appDto.NamesParams, logger *aggregates.Logger, step *entities.StepRecord, stepErr error) error {
	step.MarkAsFailure(stepErr)
	if l.presenter != nil {
		l.presenter.Step(step)
	}
	return l.loggerRepository.Save(namesParams, logger)
}

func (l *LoggerService) MarkStepAsSkipped(namesParams appDto.NamesParams, logger *aggregates.Logger, step *entities.StepRecord) error {
	step.MarkAsSkipped()
	if l.presenter != nil {
		l.presenter.Step(step)
	}
	return l.loggerRepository.Save(namesParams, logger)
}

func (l *LoggerService) MarkStepAsCached(namesParams appDto.NamesParams, logger *aggregates.Logger, step *entities.StepRecord, reason string) error {
	step.MarkAsCached(reason)
	if l.presenter != nil {
		l.presenter.Step(step)
	}
	return l.loggerRepository.Save(namesParams, logger)
}

func (l *LoggerService) MarkStepAsRunning(namesParams appDto.NamesParams, logger *aggregates.Logger, step *entities.StepRecord) error {
	step.MarkAsRunning()
	if l.presenter != nil {
		l.presenter.Step(step)
	}
	return l.loggerRepository.Save(namesParams, logger)
}

func (l *LoggerService) MarkTaskAsSuccessful(namesParams appDto.NamesParams, logger *aggregates.Logger, task *entities.TaskRecord, step *entities.StepRecord) error {
	task.MarkAsSuccess()
	if l.presenter != nil {
		l.presenter.Task(task, step)
	}
	return l.loggerRepository.Save(namesParams, logger)
}

func (l *LoggerService) MarkTaskAsFailed(namesParams appDto.NamesParams, logger *aggregates.Logger, task *entities.TaskRecord, taskErr error, step *entities.StepRecord) error {
	task.MarkAsFailure(taskErr)
	if l.presenter != nil {
		l.presenter.Task(task, step)
	}
	return l.loggerRepository.Save(namesParams, logger)
}

func (l *LoggerService) MarkTaskAsRunning(namesParams appDto.NamesParams, logger *aggregates.Logger, task *entities.TaskRecord, step *entities.StepRecord) error {
	task.MarkAsRunning()
	if l.presenter != nil {
		l.presenter.Task(task, step)
	}
	return l.loggerRepository.Save(namesParams, logger)
}

func (l *LoggerService) SetTaskCommand(namesParams appDto.NamesParams, logger *aggregates.Logger, task *entities.TaskRecord, command string) error {
	task.SetCommand(command)
	return l.loggerRepository.Save(namesParams, logger)
}

func (l *LoggerService) AddOutputToTask(namesParams appDto.NamesParams, logger *aggregates.Logger, task *entities.TaskRecord, outputLine string) error {
	task.AddOutput(outputLine)
	return l.loggerRepository.Save(namesParams, logger)
}

func (l *LoggerService) FinishExecution(namesParams appDto.NamesParams, logger *aggregates.Logger) error {
	logger.RecalculateStatus()
	if l.presenter != nil {
		l.presenter.FinalSummary(logger)
	}
	return l.loggerRepository.Save(namesParams, logger)
}
