package ports

import "github.com/jairoprogramador/vex/internal/domain/architecture/aggregates"

type QuestionRepository interface {
	GetQuestions() ([]*aggregates.Question, error)
}
