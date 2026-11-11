package rerun

import (
	"context"
	"fmt"
	"time"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/rerun"
)

const redeliveryTimeout = time.Minute

// awaitRedelivery confirms a newer pipeline for the same branch and head.
// GitHub accepting the redelivery does not establish that CircleCI built it.
// A failed read leaves the outcome unconfirmed, rather than deduplicated.
func awaitRedelivery(ctx context.Context, client *circleciclient.Client, owner, repo, branch string, doc *document, clock agentcli.Clock) error {
	observing, cancel := clock.Timeout(ctx, redeliveryTimeout)
	defer cancel()
	for {
		pipeline, err := client.FindPipelineByRevision(observing, owner, repo, branch, doc.HeadSHA)
		if err != nil {
			return microerror.Mask(fmt.Errorf("push redelivered, but could not confirm a new pipeline for head %s; recovery is unconfirmed: %w", doc.HeadSHA, err))
		}
		if err := ctx.Err(); err != nil {
			return microerror.Mask(err)
		}
		if pipeline != nil && pipeline.ID != doc.Pipeline.ID && pipeline.Number > doc.Pipeline.Number {
			doc.Redelivery.Pipeline = &rerun.Pipeline{
				ID: pipeline.ID, Number: pipeline.Number, URL: circleciclient.PipelineURL(owner, repo, pipeline.Number),
			}
			doc.Warn(fmt.Sprintf("observed new pipeline %d for head %s after the redelivery; devctl pr wait %s/%s %d follows it",
				pipeline.Number, doc.HeadSHA, owner, repo, doc.Number))
			return nil
		}
		if err := clock.Sleep(observing, 5*time.Second); err != nil {
			if err := ctx.Err(); err != nil {
				return microerror.Mask(err)
			}
			break
		}
	}
	doc.Redelivery.Outcome = rerun.OutcomeDeduplicated
	return agentcli.NewExitError(agentcli.ExitRefused, agentcli.VerdictRefused,
		"redelivery deduplicated; push a new head: no pipeline newer than %d for head %s was observed within %s; "+
			"push an empty commit to the PR branch or merge its base, then run devctl pr wait %s/%s %d; CircleCI's listing may still be delayed",
		doc.Pipeline.Number, doc.HeadSHA, redeliveryTimeout, owner, repo, doc.Number)
}
