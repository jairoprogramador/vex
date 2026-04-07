package architecture

import (
	"github.com/jairoprogramador/vex/internal/domain/architecture/aggregates"
	"github.com/jairoprogramador/vex/internal/domain/architecture/ports"
	"github.com/jairoprogramador/vex/internal/domain/architecture/vos"
)

type CacheQuestionRepository struct {
	questions []*aggregates.Question
}

func NewCacheQuestionRepository() ports.QuestionRepository {
	question1, _ := aggregates.NewQuestion(
		"¿Cómo debe comportarse tu aplicación frente a cambios de demanda?",
		[]vos.Point{
			vos.NewPoint(1, 1.0, "Fija", "La capacidad es constante, sin escalado automático"),
			vos.NewPoint(2, 1.0, "Elástica", "Escalado automático ante picos moderados"),
			vos.NewPoint(3, 1.0, "Dinámica", "Escalado automático ante picos agresivos"),
		})

	question2, _ := aggregates.NewQuestion(
		"¿Qué nivel de recuperación necesita tu sistema?",
		[]vos.Point{
			vos.NewPoint(1, 2.0, "Tolerante", "Acepta horas de caída y posible pérdida de datos"),
			vos.NewPoint(2, 2.0, "Resiliente", "Recuperación en menos de 1 hora con pérdida de datos mínima"),
			vos.NewPoint(3, 2.0, "Inmediata", "Recuperación en minutos, pérdida de datos casi nula"),
		})

	question3, _ := aggregates.NewQuestion(
		"¿Cuál es tu prioridad respecto al coste?",
		[]vos.Point{
			vos.NewPoint(-1, 1.0, "Restrictivo", "Minimizar costos, aceptando limitaciones de rendimiento"),
			vos.NewPoint(0, 1.0, "Equilibrado", "Balance entre costo y calidad de servicio"),
			vos.NewPoint(1, 1.0, "Flexible", "Priorizar rendimiento y resiliencia sobre el costo"),
		})

	questions := []*aggregates.Question{question1, question2, question3}
	return &CacheQuestionRepository{questions: questions}
}

func (r *CacheQuestionRepository) GetQuestions() ([]*aggregates.Question, error) {
	return r.questions, nil
}
