package prmerge

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

// closingReference is GitHub's closing keyword in a pull request's body —
// close, fix or resolve in any tense and case, an optional colon — directly
// before a reference: a full issue or pull URL, owner/repo#N, #N or GH-N.
// The trailing word boundary keeps `#123's` a reference to #123, as GitHub
// reads it.
var closingReference = regexp.MustCompile(`(?i)\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?):?\s+` +
	`(?:https?://github\.com/([\w.-]+)/([\w.-]+)/(issues|pull)/(\d+)|([\w.-]+)/([\w.-]+)#(\d+)|(?:#|gh-)(\d+))\b`)

// reference is one closing keyword's target in the body.
type reference struct {
	phrase string // the keyword and the reference as written
	owner  string // empty for #N and GH-N: the pull request's repository
	repo   string
	number int
	pull   bool // a /pull/ URL: a pull request whatever the lookup says
}

// closingReferences are the closing keywords' targets in body, in order.
func closingReferences(body string) []reference {
	var refs []reference
	for _, m := range closingReference.FindAllStringSubmatch(body, -1) {
		ref := reference{phrase: m[0]}
		var number string
		switch {
		case m[4] != "":
			ref.owner, ref.repo, ref.pull, number = m[1], m[2], m[3] == "pull", m[4]
		case m[7] != "":
			ref.owner, ref.repo, number = m[5], m[6], m[7]
		default:
			number = m[8]
		}
		ref.number, _ = strconv.Atoi(number)
		refs = append(refs, ref)
	}
	return refs
}

// closingWarnings name every item the body closes on the merge that is not
// an issue of the pull request's own repository: an item of another
// repository, or a pull request. GitHub closes both without a prompt, the
// pull request unmerged. A same-repository reference is looked up to tell a
// pull request from an issue; one that does not exist closes nothing, one
// that cannot be read is named as such. Warnings only: the merge proceeds.
func (m *Merger) closingWarnings(ctx context.Context, owner, repo, body string) []string {
	var warnings []string
	for _, ref := range closingReferences(body) {
		if ref.owner != "" && (!strings.EqualFold(ref.owner, owner) || !strings.EqualFold(ref.repo, repo)) {
			warnings = append(warnings, fmt.Sprintf("the body's %q closes %s/%s#%d, in another repository, when this pull request merges; write it without a closing keyword (\"Refs ...\") unless that is intended", ref.phrase, ref.owner, ref.repo, ref.number))
			continue
		}
		pull := ref.pull
		if !pull {
			var err error
			pull, err = m.github.IsPullRequest(ctx, owner, repo, ref.number)
			if githubclient.IsNotFound(err) {
				continue
			}
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("the body's %q closes #%d when this pull request merges, and whether #%d is a pull request could not be read: %s", ref.phrase, ref.number, ref.number, oneLine(err.Error())))
				continue
			}
		}
		if pull {
			warnings = append(warnings, fmt.Sprintf("the body's %q closes pull request #%d, unmerged, when this pull request merges; write it without a closing keyword (\"Refs ...\") unless that is intended", ref.phrase, ref.number))
		}
	}
	return warnings
}
