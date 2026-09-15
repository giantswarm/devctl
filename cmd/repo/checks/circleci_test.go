package checks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-github/v90/github"
	"github.com/stretchr/testify/require"
)

// generatedWorkflows is the shape `devctl gen circleci` renders for an app
// repo with branchPublish: branch jobs, tag-only release jobs and a job that
// only runs on named branches.
const generatedWorkflows = `version: 2.1
orbs:
  architect: giantswarm/architect@10.3.0
workflows:
  build:
    jobs:
    - architect/go-build:
        name: go-build
        filters:
          tags:
            only: /^v.*/
    - architect/push-to-registries:
        name: push-to-registries
        platforms: linux/amd64
        requires:
        - go-build
        filters:
          branches:
            ignore:
            - main
    - architect/push-to-registries:
        name: push-to-registries-release
        filters:
          tags:
            only: /^v.*/
          branches:
            ignore: /.*/
    - architect/push-to-app-catalog:
        name: build-chart
        filters:
          tags:
            only: /^v.*/
          branches:
            ignore:
            - main
    - architect/run-tests-with-ats:
        name: execute-chart-tests
        requires:
        - build-chart
        - push-to-registries
        filters:
          branches:
            ignore:
            - main
    - architect/push-to-app-catalog:
        name: push-chart-release
        filters:
          tags:
            only: /^v.*/
          branches:
            ignore: /.*/
    - architect/push-to-registries:
        name: push-latest
        tag-latest-branch: main
        filters:
          branches:
            only: main
`

// customWorkflows is a repo-owned custom.yml: jobs appended to the build
// workflow, one of them without parameters, and a second workflow.
const customWorkflows = `version: 2.1
jobs:
  chart-test:
    docker:
    - image: cimg/base:current
    steps:
    - checkout
workflows:
  build:
    jobs:
    - chart-test:
        filters:
          tags:
            only: /^v.*/
    - e2e-smoke
    - architect/go-test:
        name: go-test
        requires:
        - go-build
  nightly:
    jobs:
    - soak-test:
        filters:
          branches:
            only: main
`

const (
	ctxBuildChart   = "ci/circleci: build-chart"
	ctxBuildImage   = "ci/circleci: build-image"
	ctxChartTests   = "ci/circleci: execute-chart-tests"
	ctxGoBuild      = "ci/circleci: go-build"
	ctxPushRegistry = "ci/circleci: push-to-registries"
	ctxSemanticPR   = "semantic-pull-request / Validate PR title"
)

func TestCircleCIGateJobs(t *testing.T) {
	jobs, err := circleCIGateJobs([]byte(generatedWorkflows))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"go-build", "push-to-registries", "build-chart", "execute-chart-tests"}, jobs)

	jobs, err = circleCIGateJobs([]byte(customWorkflows))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"chart-test", "e2e-smoke", "go-test"}, jobs)

	_, err = circleCIGateJobs([]byte("workflows: [not: a, map"))
	require.Error(t, err)

	jobs, err = circleCIGateJobs([]byte("version: 2.1\njobs:\n  build:\n    steps: []\n"))
	require.NoError(t, err)
	require.Empty(t, jobs)
}

func TestCircleCIGateContexts(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "workflows.yml"), []byte(generatedWorkflows), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "custom.yml"), []byte(customWorkflows), 0o600))

	contexts, found, err := circleCIGateContexts(dir)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []string{
		ctxBuildChart,
		"ci/circleci: chart-test",
		"ci/circleci: e2e-smoke",
		ctxChartTests,
		ctxGoBuild,
		"ci/circleci: go-test",
		ctxPushRegistry,
	}, contexts)

	// Only the generated file: the same result minus the custom jobs.
	require.NoError(t, os.Remove(filepath.Join(dir, "custom.yml")))
	contexts, found, err = circleCIGateContexts(dir)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []string{ctxBuildChart, ctxChartTests, ctxGoBuild, ctxPushRegistry}, contexts)

	// No pipeline at all is reported, not an error, so the caller leaves the
	// contexts alone.
	empty := t.TempDir()
	contexts, found, err = circleCIGateContexts(empty)
	require.NoError(t, err)
	require.False(t, found)
	require.Empty(t, contexts)

	// An unreadable pipeline is an error: nothing may be removed on a guess.
	require.NoError(t, os.WriteFile(filepath.Join(empty, "workflows.yml"), []byte("workflows: [oops"), 0o600))
	_, found, err = circleCIGateContexts(empty)
	require.Error(t, err)
	require.True(t, found)
}

func TestStaleCircleCIContexts(t *testing.T) {
	existing := []*github.RequiredStatusCheck{
		{Context: ctxGoBuild},
		{Context: ctxBuildImage},
		{Context: ctxPushRegistry},
		{Context: "pre-commit"},
		{Context: ctxSemanticPR},
	}
	live := []string{ctxGoBuild, ctxPushRegistry, ctxBuildChart}

	// The renamed job's old context goes; other systems' contexts and the live
	// jobs stay, and a live job that is not required yet is not this function's
	// business.
	require.Equal(t, []string{ctxBuildImage}, staleCircleCIContexts(existing, live))
	require.Empty(t, staleCircleCIContexts(existing[3:], nil))
	require.Equal(t, []string{ctxGoBuild, ctxBuildImage, ctxPushRegistry}, staleCircleCIContexts(existing, nil))
}
