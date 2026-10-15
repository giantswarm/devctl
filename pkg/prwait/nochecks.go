package prwait

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

// NoChecksGrace is how long a head may carry no check run, no status and no
// Actions run while the repository has workflows a pull_request event runs:
// the time GitHub takes to deliver the event and register their runs. It is
// measured from the pull request's last update, which a push to its head is.
const NoChecksGrace = 2 * time.Minute

// pullRequestEvent is the workflow trigger a push to a pull request fires.
const pullRequestEvent = "pull_request"

// workflowsDir is where GitHub reads a repository's Actions workflows from.
const workflowsDir = ".github/workflows"

// skipCIMarkers in a head commit's message make GitHub skip the push and
// pull_request workflows of that commit.
var skipCIMarkers = []string{"[skip ci]", "[ci skip]", "[no ci]", "[skip actions]", "[actions skip]"}

// noChecksError is the verdict of a head that carries nothing at all past
// NoChecksGrace although the repository's workflows run on pull_request: the
// event that starts them never arrived, and no wait makes it arrive.
func noChecksError(sha string, workflows []string) error {
	return agentcli.NewExitError(agentcli.ExitRequiredMissing, agentcli.VerdictNoChecks,
		"the head %s has no check run, status or Actions run more than %s after the pull request's last update, while %s run on pull_request: GitHub did not deliver the pull_request event; close and reopen the pull request to trigger them, then wait again",
		sha, NoChecksGrace, strings.Join(workflows, ", "))
}

// empty: the head carries no check run, no status, no Actions run and no
// CircleCI pipeline.
func (e *evaluation) empty() bool {
	return len(e.checks) == 0 && len(e.actions) == 0 && e.circleci == nil
}

// expectPullRequestRuns marks an empty, otherwise green head as waiting for
// the runs of the repository's pull_request workflows, and records how long
// the pull request has been quiet. A head GitHub skips CI for, and a
// repository without such a workflow, stay green: nothing is going to report.
func (w *Waiter) expectPullRequestRuns(ctx context.Context, owner, repo string, h *head, updatedAt time.Time, e *evaluation) error {
	if !e.green() || !e.empty() {
		return nil
	}
	workflows, err := w.pullRequestWorkflows(ctx, owner, repo, h)
	if err != nil || len(workflows) == 0 {
		return err
	}
	e.expectedRuns = workflows
	e.quietFor = w.clock.Now().Sub(updatedAt)
	e.unfinished = append(e.unfinished, fmt.Sprintf("actions runs of %s (none registered)", strings.Join(workflows, ", ")))
	return nil
}

// noChecks: the head still carries nothing past the grace.
func (w *Waiter) noChecks(e *evaluation) bool {
	return len(e.expectedRuns) > 0 && e.quietFor >= w.clock.Scaled(NoChecksGrace)
}

// pullRequestWorkflows returns the paths of the active workflows of the head
// that a pull_request event against the base runs, read once per head and
// base. A head whose commit message skips CI has none.
func (w *Waiter) pullRequestWorkflows(ctx context.Context, owner, repo string, h *head) ([]string, error) {
	if h.workflowsRead {
		return h.workflows, nil
	}
	workflows, err := w.readPullRequestWorkflows(ctx, owner, repo, h.sha, h.baseRef)
	if err != nil {
		return nil, err
	}
	h.workflows, h.workflowsRead = workflows, true
	return workflows, nil
}

func (w *Waiter) readPullRequestWorkflows(ctx context.Context, owner, repo, sha, baseRef string) ([]string, error) {
	message, err := w.github.CommitMessage(ctx, owner, repo, sha)
	if err != nil {
		return nil, err
	}
	if skipsCI(message) {
		w.progress.Printf("the head commit's message skips CI: no pull_request run expected")
		return nil, nil
	}
	names, err := w.github.ListDirectory(ctx, owner, repo, workflowsDir, sha)
	if githubclient.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var candidates []string
	for _, name := range names {
		if ext := path.Ext(name); ext == ".yml" || ext == ".yaml" {
			candidates = append(candidates, workflowsDir+"/"+name)
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	inactive, err := w.github.InactiveWorkflowPaths(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	var workflows []string
	for _, file := range candidates {
		if inactive[file] {
			continue
		}
		content, found, err := w.github.ReadFile(ctx, owner, repo, file, sha)
		if err != nil {
			return nil, err
		}
		if found && runsOnPullRequest(content, baseRef) {
			workflows = append(workflows, file)
		}
	}
	return workflows, nil
}

// skipsCI: the commit message carries one of GitHub's skip instructions.
func skipsCI(message string) bool {
	lower := strings.ToLower(message)
	for _, marker := range skipCIMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	for _, line := range strings.Split(lower, "\n") {
		if key, value, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(key) == "skip-checks" && strings.TrimSpace(value) == "true" {
			return true
		}
	}
	return false
}

// pullRequestFilter is the part of a workflow's pull_request trigger that
// decides whether a pull request's push runs it.
type pullRequestFilter struct {
	Types          []string `yaml:"types"`
	Branches       []string `yaml:"branches"`
	BranchesIgnore []string `yaml:"branches-ignore"`
	Paths          []string `yaml:"paths"`
	PathsIgnore    []string `yaml:"paths-ignore"`
}

// runsOnPullRequest: the workflow is certain to run for every push to a pull
// request against base. A trigger whose filters may leave it out (paths, a
// branch pattern this cannot match, types without opened and synchronize),
// and a file that does not parse, count as not running: the wait would
// rather call an empty head green, as it did, than refuse one forever.
func runsOnPullRequest(content []byte, base string) bool {
	var doc struct {
		On yaml.Node `yaml:"on"`
	}
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return false
	}
	on := doc.On
	switch on.Kind {
	case yaml.ScalarNode:
		return on.Value == pullRequestEvent
	case yaml.SequenceNode:
		return slices.ContainsFunc(on.Content, func(n *yaml.Node) bool { return n.Kind == yaml.ScalarNode && n.Value == pullRequestEvent })
	case yaml.MappingNode:
		for i := 0; i+1 < len(on.Content); i += 2 {
			if on.Content[i].Value != pullRequestEvent {
				continue
			}
			trigger := on.Content[i+1]
			if trigger.Kind == yaml.ScalarNode && (trigger.Tag == "!!null" || trigger.Value == "") {
				return true
			}
			var filter pullRequestFilter
			if err := trigger.Decode(&filter); err != nil {
				return false
			}
			return filter.matches(base)
		}
	}
	return false
}

func (f pullRequestFilter) matches(base string) bool {
	if len(f.Paths) > 0 || len(f.PathsIgnore) > 0 {
		return false
	}
	if len(f.Types) > 0 && (!slices.Contains(f.Types, "opened") || !slices.Contains(f.Types, "synchronize")) {
		return false
	}
	if len(f.Branches) > 0 {
		matched, known := matchBranch(f.Branches, base)
		if !known || !matched {
			return false
		}
	}
	if len(f.BranchesIgnore) > 0 {
		matched, known := matchBranch(f.BranchesIgnore, base)
		if !known || matched {
			return false
		}
	}
	return true
}

// matchBranch says whether base matches one of the patterns; known is false
// when a pattern uses more of GitHub's filter syntax than * and ** (?, +,
// character ranges, negation), which this does not judge.
func matchBranch(patterns []string, base string) (matched, known bool) {
	for _, pattern := range patterns {
		if strings.ContainsAny(pattern, `?+[]!\`) {
			return false, false
		}
	}
	for _, pattern := range patterns {
		if branchPattern(pattern).MatchString(base) {
			return true, true
		}
	}
	return false, true
}

// branchPattern turns a filter pattern of literals, * (anything but /) and
// ** (anything) into an anchored regular expression.
func branchPattern(pattern string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	for i, part := range strings.Split(pattern, "**") {
		if i > 0 {
			b.WriteString(".*")
		}
		for j, literal := range strings.Split(part, "*") {
			if j > 0 {
				b.WriteString("[^/]*")
			}
			b.WriteString(regexp.QuoteMeta(literal))
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String())
}
