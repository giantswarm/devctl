package githubclient

import (
	"context"
	"fmt"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v93/github"
)

const (
	publishFailureStart = "<!-- devctl:publish-failed -->"
	publishFailureEnd   = "<!-- /devctl:publish-failed -->"
)

func hasPublishFailure(body string) bool {
	_, notice, found := strings.Cut(body, publishFailureStart+"\n")
	return found && strings.Contains(notice, publishFailureEnd+"\n\n")
}

// EnsureReleasePublishStatus records a failed tag pipeline in a release's
// notes, or removes devctl's notice once the tag is available. Only the notes
// are edited; the tag, title, draft and prerelease state are preserved.
func (c *Client) EnsureReleasePublishStatus(ctx context.Context, owner, repo, tag, pipelineURL string, failed bool) error {
	rel, _, err := c.ghClient.Repositories.GetReleaseByTag(ctx, owner, repo, tag)
	if isGithub404(err) && !failed {
		return nil
	}
	if err != nil {
		return microerror.Mask(err)
	}
	if rel.GetID() == 0 {
		return microerror.Maskf(executionError, "release %s has no id", tag)
	}
	body := rel.GetBody()
	notes := body
	if before, notice, found := strings.Cut(notes, publishFailureStart); found {
		_, rest, ok := strings.Cut(notice, publishFailureEnd+"\n\n")
		if !ok {
			return microerror.Maskf(executionError, "release %s has an incomplete devctl publish notice; notes were preserved", tag)
		}
		notes = before + rest
	}
	if failed {
		notes = fmt.Sprintf("%s\n> **Not published:** The [tag pipeline](%s) failed. Release artifacts may be missing or incomplete.\n%s\n\n%s", publishFailureStart, pipelineURL, publishFailureEnd, notes)
	}
	if notes == body {
		return nil
	}
	_, _, err = c.ghClient.Repositories.UpdateRelease(ctx, owner, repo, rel.GetID(), github.UpdateReleaseRequest{Body: &notes})
	return microerror.Mask(err)
}
