package ports

import "context"

type GitInfo interface {
	RemoteURL(ctx context.Context, dir string) (string, error)
	CurrentRef(ctx context.Context, dir string) (string, error)
}
