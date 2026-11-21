package ports

type Version interface {
	GetLatest() (string, error)
}
