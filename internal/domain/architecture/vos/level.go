package vos

type Level struct {
	name  string
	value int
}

func NewLevel(name string, value int) Level {
	return Level{name: name, value: value}
}

func (l Level) Name() string {
	return l.name
}

func (l Level) Value() int {
	return l.value
}
