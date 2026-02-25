package vos

type Point struct {
	value       int
	message     string
	description string
	weight      float64
}

func NewPoint(value int, weight float64, message, description string) Point {
	return Point{value: value, message: message, description: description, weight: weight}
}

func (p Point) Value() int {
	return p.value
}

func (p Point) Message() string {
	return p.message
}

func (p Point) Description() string {
	return p.description
}

func (p Point) Weight() float64 {
	return p.weight
}
