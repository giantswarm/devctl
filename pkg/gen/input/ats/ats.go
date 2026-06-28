package ats

import (
	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/ats/internal/file"
)

// CreateATS returns the inputs for the canonical app-test-suite (ATS) test
// dependencies: tests/ats/Pipfile. Emission is gated on the chart/app
// (.HasApp) flavour by the caller (devctl gen circleci) -- the same signal that
// emits the run-tests-with-ats jobs -- so the Pipfile never lands in a non-app
// or non-generated-CI repo.
func CreateATS() []input.Input {
	return []input.Input{
		file.NewCreatePipfileInput(),
	}
}
