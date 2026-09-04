package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
)

//go:embed pyproject.toml
var createPyproject string

//go:embed uv.lock
var createUVLock string

// NewCreatePyprojectInput emits tests/ats/pyproject.toml: the canonical,
// centrally pinned app-test-suite (ATS) test dependency set in the layout ATS
// 1.x consumes (`uv sync` / `uv run pytest`). Like the Pipfile it carries no
// per-repo substitution and skips the regen check, so a central bump overwrites
// the repo copy on the next align run.
func NewCreatePyprojectInput() input.Input {
	return input.Input{
		Path:           "tests/ats/pyproject.toml",
		TemplateBody:   createPyproject,
		SkipRegenCheck: true,
	}
}

// NewCreateUVLockInput emits tests/ats/uv.lock, the lock file resolved from the
// canonical pyproject.toml (`uv lock` in pkg/gen/input/ats/internal/file). ATS
// runs `uv sync` against it, so the two files are bumped and shipped together.
func NewCreateUVLockInput() input.Input {
	return input.Input{
		Path:           "tests/ats/uv.lock",
		TemplateBody:   createUVLock,
		SkipRegenCheck: true,
	}
}

// NewDeletePipfileInputs removes the pipenv layout (tests/ats/Pipfile and a
// stale tests/ats/Pipfile.lock) once a repo is on the uv layout: ATS 1.x ignores
// a Pipfile, and leaving the generator's old output behind would only confuse.
func NewDeletePipfileInputs() []input.Input {
	return []input.Input{
		{Path: "tests/ats/Pipfile", Delete: true},
		{Path: "tests/ats/Pipfile.lock", Delete: true},
	}
}
