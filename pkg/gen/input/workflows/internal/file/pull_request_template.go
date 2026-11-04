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

// changelogChecklistItem matches a checklist item that asks for a CHANGELOG.md
// edit, ticked or not, and the line an earlier devctl replaced it with, each
// with its line break.
var changelogChecklistItem = regexp.MustCompile(`(?mi)^[ \t]*(?:[-*] \[[ x]\] .*changelog.*|The pull request title is the release note: the release writes CHANGELOG\.md, so do not edit it\.)(?:\r?\n|$)`)

// NewPullRequestTemplateInput drops the checklist item asking for a
// CHANGELOG.md edit from the pull request template of a repository on the
// auto-release flow, whose GitHub Releases are the changelog: an edit to the
// file brings back the conflicts between parallel pull requests. The rest of
// the template stays as the repository has it.
func NewPullRequestTemplateInput() input.Input {
	return input.Input{
		Path:           PullRequestTemplatePath,
		SkipRegenCheck: true,
		Generate: func(_ context.Context) ([]byte, error) {
			content, err := os.ReadFile(PullRequestTemplatePath)
			if err != nil {
				return nil, microerror.Mask(err)
			}

			return changelogChecklistItem.ReplaceAll(content, nil), nil
		},
	}
}
