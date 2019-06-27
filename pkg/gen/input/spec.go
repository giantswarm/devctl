package input

import "context"

type File interface {
	GetInput(ctx context.Context) (Input, error)
}
