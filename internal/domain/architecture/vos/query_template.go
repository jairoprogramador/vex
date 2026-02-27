package vos

type QueryTemplate struct {
	stack    string
	platform string
	level    int
	cost     int
}

type QueryOption func(*QueryTemplate)

func WithStack(stack string) QueryOption {
	return func(q *QueryTemplate) { q.stack = stack }
}

func WithPlatform(platform string) QueryOption {
	return func(q *QueryTemplate) { q.platform = platform }
}

func WithLevel(level int) QueryOption {
	return func(q *QueryTemplate) { q.level = level }
}

func WithCost(cost int) QueryOption {
	return func(q *QueryTemplate) { q.cost = cost }
}

func NewQueryTemplate(opts ...QueryOption) QueryTemplate {
	q := QueryTemplate{}
	for _, opt := range opts {
		opt(&q)
	}
	return q
}

func (q QueryTemplate) Stack() string    { return q.stack }
func (q QueryTemplate) Platform() string { return q.platform }
func (q QueryTemplate) Level() int       { return q.level }
func (q QueryTemplate) Cost() int        { return q.cost }
