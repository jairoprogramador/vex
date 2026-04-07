package aggregates

import (
	"errors"
	"github.com/jairoprogramador/vex/internal/domain/architecture/vos"
)

type Question struct {
	ask    string
	points []vos.Point
}

func NewQuestion(ask string, points []vos.Point) (*Question, error) {
	if len(points) == 0 {
		return nil, errors.New("points are required")
	}

	values := make(map[int]bool)
	for _, point := range points {
		if values[point.Value()] {
			return nil, errors.New("points must have unique values")
		}
		values[point.Value()] = true
	}

	return &Question{ask: ask, points: points}, nil
}

func (q Question) Ask() string {
	return q.ask
}

func (q Question) Points() []vos.Point {
	return q.points
}
