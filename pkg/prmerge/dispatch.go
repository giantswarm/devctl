package prmerge

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/giantswarm/microerror"
)

// EnvDispatch is the environment variable that configures the workflow
// dispatch of every pr merge on a machine, in the form of --dispatch; the
// flag wins over it.
const EnvDispatch = "DEVCTL_MERGE_DISPATCH"

// The workflow_dispatch inputs the dispatch carries; the workflow declares
// them, GitHub refuses inputs a workflow does not declare.
const (
	InputRepository  = "repository"
	InputPullRequest = "pull_request"
	InputRelease     = "release"
)

// Dispatch is the workflow a merge dispatches afterwards: the workflow file
// of a repository, run on Ref through its workflow_dispatch trigger. An empty
// Ref is the repository's default branch, read before the dispatch.
type Dispatch struct {
	Owner string
	Repo  string
	File  string
	Ref   string
}

// ParseDispatch reads "<owner>/<repo>/<workflow file>[@<ref>]", the form of
// --dispatch and DEVCTL_MERGE_DISPATCH: the file is the name of a workflow in
// .github/workflows of that repository.
func ParseDispatch(s string) (*Dispatch, error) {
	target, ref, _ := strings.Cut(s, "@")
	parts := strings.Split(target, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" || strings.ContainsAny(ref, "/ ") {
		return nil, microerror.Maskf(invalidConfigError, "the dispatch is <owner>/<repo>/<workflow file>[@<ref>], got %q", s)
	}
	return &Dispatch{Owner: parts[0], Repo: parts[1], File: parts[2], Ref: ref}, nil
}

// String is the dispatch in the form ParseDispatch reads.
func (d Dispatch) String() string {
	s := d.Owner + "/" + d.Repo + "/" + d.File
	if d.Ref != "" {
		s += "@" + d.Ref
	}
	return s
}

// DispatchResult is the document's account of the workflow dispatch after the
// merge.
type DispatchResult struct {
	// Workflow is the dispatched workflow as <owner>/<repo>/<file>.
	Workflow string `json:"workflow"`
	// Ref is the branch or tag the workflow ran on: the one asked for, or
	// the repository's default branch.
	Ref string `json:"ref"`
	// Inputs are the workflow_dispatch inputs sent: repository, pull_request
	// and release (empty when no release follows the merge or none was
	// waited for).
	Inputs map[string]string `json:"inputs"`
	// Dispatched: GitHub accepted the dispatch. False with Reason when it
	// did not; the merge's outcome is unchanged either way.
	Dispatched bool `json:"dispatched"`
	// Reason is why the dispatch failed; empty when it was accepted.
	Reason string `json:"reason"`
}

// dispatch runs the configured workflow after a merge, with the merged pull
// request and its release as inputs, and records the outcome in result and
// its warnings. A dispatch that fails never changes the merge's outcome.
func (m *Merger) dispatch(ctx context.Context, result *Result) {
	if m.dispatchTo == nil || result.MergeCommitSHA == "" {
		return
	}
	d := *m.dispatchTo
	inputs := map[string]string{
		InputRepository:  result.Repository,
		InputPullRequest: strconv.Itoa(result.Number),
		InputRelease:     "",
	}
	if result.Release != nil {
		inputs[InputRelease] = result.Release.Tag
	}
	out := &DispatchResult{Workflow: Dispatch{Owner: d.Owner, Repo: d.Repo, File: d.File}.String(), Ref: d.Ref, Inputs: inputs}
	result.Dispatch = out

	err := m.dispatchWorkflow(ctx, d, out)
	if err != nil {
		out.Reason = oneLine(err.Error())
		w := fmt.Sprintf("the dispatch of %s after the merge failed: %s", out.Workflow, err)
		m.progress.Printf("warning: %s", w)
		result.Warnings = append(result.Warnings, w)
		return
	}
	out.Dispatched = true
	m.progress.Printf("dispatched: %s on %s with %s", out.Workflow, out.Ref, formatInputs(inputs))
}

// dispatchWorkflow resolves the ref and sends the dispatch; out.Ref is set
// to the ref used.
func (m *Merger) dispatchWorkflow(ctx context.Context, d Dispatch, out *DispatchResult) error {
	if out.Ref == "" {
		ref, err := m.github.DefaultBranch(ctx, d.Owner, d.Repo)
		if err != nil {
			return fmt.Errorf("reading the default branch of %s/%s: %w", d.Owner, d.Repo, err)
		}
		out.Ref = ref
	}
	inputs := make(map[string]any, len(out.Inputs))
	for k, v := range out.Inputs {
		inputs[k] = v
	}
	if err := m.github.DispatchWorkflow(ctx, d.Owner, d.Repo, d.File, out.Ref, inputs); err != nil {
		return fmt.Errorf("on %s: %w", out.Ref, err)
	}
	return nil
}

// formatInputs is "k=v k=v" in key order, for the progress line.
func formatInputs(inputs map[string]string) string {
	keys := make([]string, 0, len(inputs))
	for k := range inputs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+inputs[k])
	}
	return strings.Join(parts, " ")
}
