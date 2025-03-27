package params

import (
	"github.com/giantswarm/devctl/v7/pkg/gen/internal"
)

func Header(comment, githubUrl string) string {
	return internal.Header(comment, githubUrl)
}
