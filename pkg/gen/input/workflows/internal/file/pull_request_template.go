package file

import (
	"context"
	"os"
	"regexp"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
)

// PullRequestTemplatePath is where a repository keeps its pull request
// template. The file is the repository's own, from giantswarm/template or
// written by hand.
const PullRequestTemplatePath = ".github/pull_request_template.md"

const pullRequestTemplateChangelogLine = "The pull request title is the release note: the release writes CHANGELOG.md, so do not edit it."

// changelogChecklistItem matches a checklist item that asks for a CHANGELOG.md
// edit, ticked or not.
var changelogChecklistItem = regexp.MustCompile(`(?mi)^[ \t]*[-*] \[[ x]\] .*changelog.*$`)

// NewPullRequestTemplateInput rewrites the pull request template of a
// repository on the auto-release flow, whose release writes CHANGELOG.md: a
// checklist item asking for a CHANGELOG.md edit brings back the conflicts
// between parallel pull requests the flow removes. The rest of the template
// stays as the repository has it.
func NewPullRequestTemplateInput() input.Input {
	return input.Input{
		Path:           PullRequestTemplatePath,
		SkipRegenCheck: true,
		Generate: func(_ context.Context) ([]byte, error) {
			content, err := os.ReadFile(PullRequestTemplatePath)
			if err != nil {
				return nil, microerror.Mask(err)
			}

			return changelogChecklistItem.ReplaceAll(content, []byte(pullRequestTemplateChangelogLine)), nil
		},
	}
}
