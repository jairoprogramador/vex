package logger

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	appDto "github.com/jairoprogramador/vex/internal/application/dto"

	logAgg "github.com/jairoprogramador/vex/internal/domain/logger/aggregates"
	logPor "github.com/jairoprogramador/vex/internal/domain/logger/ports"

	ilogDto "github.com/jairoprogramador/vex/internal/infrastructure/logger/dto"
	ilogMap "github.com/jairoprogramador/vex/internal/infrastructure/logger/mapper"
)

type FileLoggerRepository struct {
	pathStateRoot string
}

func NewFileLoggerRepository(
	pathStateRoot string,
) logPor.LoggerRepository {
	return &FileLoggerRepository{pathStateRoot: pathStateRoot}
}

func (r *FileLoggerRepository) getPathFile(namesParams appDto.NamesParams) (string, error) {
	pathLogger := filepath.Join(r.pathStateRoot, namesParams.ProjectName(), namesParams.RepositoryName(), "logs")
	if err := os.MkdirAll(pathLogger, 0755); err != nil {
		return "", err
	}
	pathLogger = filepath.Join(pathLogger, "logger.yaml")
	return pathLogger, nil
}

func (r *FileLoggerRepository) Save(namesParams appDto.NamesParams, log *logAgg.Logger) error {
	filePath, err := r.getPathFile(namesParams)
	if err != nil {
		return err
	}

	loggerDto := ilogMap.LoggerToDTO(log)
	data, err := yaml.Marshal(loggerDto)
	if err != nil {
		return fmt.Errorf("failed to marshal execution log to yaml: %w", err)
	}

	return os.WriteFile(filePath, data, 0644)
}

func (r *FileLoggerRepository) Find(namesParams appDto.NamesParams) (logAgg.Logger, error) {
	filePath, err := r.getPathFile(namesParams)
	if err != nil {
		return logAgg.Logger{}, err
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return logAgg.Logger{}, fmt.Errorf("could not read log file: %w", err)
	}

	var loggerDto ilogDto.LoggerDTO
	if err := yaml.Unmarshal(data, &loggerDto); err != nil {
		return logAgg.Logger{}, fmt.Errorf("failed to unmarshal execution log from yaml: %w", err)
	}

	logger, err := ilogMap.LoggerToDomain(loggerDto)
	if err != nil {
		return logAgg.Logger{}, err
	}

	return *logger, nil
}
