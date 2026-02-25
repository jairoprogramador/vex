package services

import (
	"errors"
	"github.com/jairoprogramador/vex-client/internal/domain/architecture/aggregates"
	"github.com/jairoprogramador/vex-client/internal/domain/architecture/vos"
)

func Score(response []int, questions []*aggregates.Question) (float64, error) {
	if len(questions) != len(response) {
		return 0.0, errors.New("the number of questions and responses must be the same")
	}

	score := 0.0
	for index, question := range questions {
		valueResponse := response[index]
		weight, ok := findWeight(question.Points(), valueResponse)
		if !ok {
			return 0.0, errors.New("point not found")
		}
		score += float64(valueResponse) * weight
	}
	return score, nil
}

func scoreMax(questions []*aggregates.Question) float64 {
	score := 0.0
	for _, question := range questions {
		indexMax := len(question.Points()) - 1
		pointMax := question.Points()[indexMax]
		score += float64(pointMax.Value()) * pointMax.Weight()
	}
	return score
}

func ScoreMin(questions []*aggregates.Question) float64 {
	score := 0.0
	indexMin := 0
	for _, question := range questions {
		pointMin := question.Points()[indexMin]
		score += float64(pointMin.Value()) * pointMin.Weight()
	}
	return score
}

func findWeight(points []vos.Point, value int) (float64, bool) {
	for _, point := range points {
		if point.Value() == value {
			return point.Weight(), true
		}
	}
	return 1.0, false
}

func Level(score float64, levels []vos.Level, questions []*aggregates.Question) (vos.Level, error) {
	minScore := ScoreMin(questions)
	coefficient := coefficient(questions)

	indexLevelMax := len(levels) - 1

	for index, level := range levels {
		criticalPointInitial, criticalPointFinal := levelRange(level.Value(), minScore, coefficient)

		if index == indexLevelMax {
			criticalPointFinal = scoreMax(questions) + 1
		}

		if score >= criticalPointInitial && score < criticalPointFinal {
			return level, nil
		}
	}
	return vos.Level{}, errors.New("level not found")
}

func levelRange(valueLevel int, minScore float64, coefficient float64) (float64, float64) {
	criticalPointInitial := criticalPoint(valueLevel, minScore, coefficient)
	criticalPointFinal := criticalPoint(valueLevel+1, minScore, coefficient)

	return criticalPointInitial, criticalPointFinal

}

func criticalPoint(level int, minScore float64, coefficient float64) float64 {
	return minScore + float64(level)*coefficient
}

func coefficient(questions []*aggregates.Question) float64 {
	return (scoreMax(questions) - ScoreMin(questions)) / 3.0
}
