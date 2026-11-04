package file

import (
	"context"
	"os"
	"regexp"
	"strings"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
)

// PullRequestTemplatePath is where a repository keeps its pull request
// template. The file is the repository's own, from giantswarm/template or
// written by hand.
const PullRequestTemplatePath = ".github/pull_request_template.md"

// changelogChecklistItem matches a checklist item that asks for a CHANGELOG.md
// edit, ticked or not, and the line an earlier devctl replaced it with.
var changelogChecklistItem = regexp.MustCompile(`(?i)^[ \t]*(?:[-*] \[[ x]\] .*changelog.*|The pull request title is the release note: the release writes CHANGELOG\.md, so do not edit it\.)[ \t]*\r?\n?$`)

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

			return []byte(dropChangelogItem(string(content))), nil
		},
	}
}

// dropChangelogItem removes the changelog checklist items from a pull request
// template. A heading whose section held nothing else, such as a checklist
// with this one item, goes with them.
func dropChangelogItem(template string) string {
	lines := strings.SplitAfter(template, "\n")

	var kept []string
	emptied := map[int]bool{}
	heading := -1
	for _, line := range lines {
		if changelogChecklistItem.MatchString(line) {
			if heading >= 0 {
				emptied[heading] = true
			}
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			heading = len(kept)
		}
		kept = append(kept, line)
	}
	if len(kept) == len(lines) {
		return template
	}

	var out []string
	for i := 0; i < len(kept); i++ {
		if emptied[i] {
			end := i + 1
			for end < len(kept) && strings.TrimSpace(kept[end]) == "" {
				end++
			}
			if end == len(kept) || strings.HasPrefix(strings.TrimSpace(kept[end]), "#") {
				i = end - 1
				continue
			}
		}
		out = append(out, kept[i])
	}

	result := strings.Join(out, "")
	if trimmed := strings.TrimRight(result, " \t\r\n"); trimmed != "" {
		return trimmed + "\n"
	}
	return ""
}
