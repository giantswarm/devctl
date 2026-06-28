package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
)

//go:embed Pipfile
var createPipfile string

// NewCreatePipfileInput emits tests/ats/Pipfile: the canonical, centrally
// pinned app-test-suite (ATS) test dependency set. It carries no per-repo
// substitution -- the file is emitted verbatim from the embedded source so the
// copy in every chart/app repo stays byte-identical to the one Renovate's
// pipenv manager bumps within giantswarm/devctl.
//
// SkipRegenCheck keeps the file overwritten on every align run (a plain Pipfile
// is not otherwise regenerable, so without it an existing repo Pipfile would
// never be updated), which is what makes a central bump reliably propagate.
func NewCreatePipfileInput() input.Input {
	return input.Input{
		Path:           "tests/ats/Pipfile",
		TemplateBody:   createPipfile,
		SkipRegenCheck: true,
	}
}
