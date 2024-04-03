package params

import (
	"github.com/giantswarm/devctl/v6/pkg/gen/internal"
)

func Header(comment, githubUrl string) string {
	return internal.Header(comment, githubUrl)
}
