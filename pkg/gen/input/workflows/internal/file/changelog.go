package file

import (
	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

// NewChangelogDeletionInput deletes .github/workflows/zz_generated.changelog.yaml,
// the retired pull request check on released entries under Unreleased, from
// every repository a gen run touches.
func NewChangelogDeletionInput(p params.Params) input.Input {
	return input.Input{
		Delete: true,
		Path:   params.RegenerableFileName(p, "changelog.yaml"),
	}
}
