package releasewait

import (
	"context"
	"fmt"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

// RecordPublishStatus is the CLI's write after a wait. The waiter itself and
// its read-only server callers never write release notes. A commit status
// alone cannot identify the tag's pipeline (a candidate and stable tag can
// share a commit), so only a verified CircleCI pipeline may mark a release.
// Recording failure does not replace the wait's outcome; callers report it
// as a warning instead. A successful wait clears a previous failure notice.
func RecordPublishStatus(ctx context.Context, client *githubclient.Client, result Result, waitErr error) error {
	if client == nil || result.Tag == "" || result.Pipeline == nil || result.Pipeline.ID == "" {
		return nil
	}
	code, verdict := agentcli.Outcome(waitErr)
	failed := code == agentcli.ExitRed && verdict == agentcli.VerdictCIFailed
	if waitErr != nil && !failed && code != agentcli.ExitStatusFailed {
		return nil
	}
	owner, repo, err := agentcli.ParseRepository(result.Repository)
	if err != nil {
		return err
	}
	// The parent's cancellation still applies. A notes API outage must not
	// hold the outcome of an already completed wait indefinitely.
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := client.EnsureReleasePublishStatus(ctx, owner, repo, result.Tag, result.Pipeline.URL, failed); err != nil {
		return fmt.Errorf("could not record publish status of %s: %w (editing release notes requires Contents: write)", result.Tag, err)
	}
	return nil
}
