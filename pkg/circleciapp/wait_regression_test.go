package circleciapp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

func TestWaitFollowsAFailingWorkflowUntilItFinishes(t *testing.T) {
	ci := newFake()
	ci.reads = []pipelineRead{
		{state: "created", workflows: []circleciclient.Workflow{{Name: "build", Status: "failing"}}},
		{state: "created", workflows: []circleciclient.Workflow{{Name: "build", Status: "failed"}}},
	}

	outcome, err := Waiter{CircleCI: ci, Sleep: noSleep}.Wait(context.Background(), "pl1")
	require.NoError(t, err)
	require.Equal(t, 2, ci.read, "failing still has running jobs and does not settle the wait")
	require.False(t, outcome.Green)
	require.Equal(t, []string{"build (failed)"}, outcome.Failed)
}
