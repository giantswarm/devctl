package ats

import (
	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/ats/internal/file"
)

// CreateATS returns the inputs for the canonical app-test-suite (ATS) test
// dependencies. Emission is gated on the chart/app (.HasApp) flavour by the
// caller (devctl gen circleci) -- the same signal that emits the
// run-tests-with-ats jobs -- so the files never land in a non-app or
// non-generated-CI repo.
//
// uv selects the layout: false is the pipenv layout ATS up to 0.15 consumes
// (tests/ats/Pipfile); true is the uv layout of ATS 1.x (tests/ats/pyproject.toml
// + uv.lock), which also removes the Pipfile the generator used to emit.
func CreateATS(uv bool) []input.Input {
	if !uv {
		return []input.Input{
			file.NewCreatePipfileInput(),
		}
	}

	inputs := []input.Input{
		file.NewCreatePyprojectInput(),
		file.NewCreateUVLockInput(),
	}
	return append(inputs, file.NewDeletePipfileInputs()...)
}
