package releasewait

import (
	"context"
	"fmt"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/circleciconfig"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

// emptyPipelineError ends the wait on a settled pipeline without workflows
// only when the configuration at the tag says jobs should run. A tag the
// filters exclude, or a condition the reader cannot resolve, proves no
// missing workflow. Setup and continuation states never reach this check.
func (w *Waiter) emptyPipelineError(ctx context.Context, result *Result, state string) error {
	read := func(ctx context.Context, path string) ([]byte, bool, error) {
		file, err := w.config.GitHub.GetFile(ctx, w.config.Owner, w.config.Repo, path, result.SHA)
		if githubclient.IsNotFound(err) {
			return nil, false, nil
		}
		return file.Data, err == nil, err
	}
	jobs, _, err := circleciconfig.List(ctx, read, circleciconfig.Run{Tag: result.Tag})
	if err != nil {
		return fmt.Errorf("reading the expected CircleCI jobs of %s: %w", result.Tag, err)
	}
	if len(jobs.Names) == 0 {
		return nil
	}
	return agentcli.NewExitError(agentcli.ExitTagPipelineEmpty, agentcli.VerdictTagPipelineEmpty,
		"the tag pipeline %d (%s) of %s is %s with no workflows although its configuration admits the tag; re-trigger the pipeline for tag %s",
		result.Pipeline.Number, result.Pipeline.URL, result.Tag, state, result.Tag)
}
