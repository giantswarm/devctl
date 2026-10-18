package releasepromote

import (
	"fmt"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// The states of one repository.
const (
	// StateDispatched: the workflow was dispatched with release-type stable.
	StateDispatched = "dispatched"
	// StateWouldDispatch: with --dry-run, the workflow would be dispatched.
	StateWouldDispatch = "would_dispatch"
	// StateNothingToPromote: no candidate pre-release newer than the latest
	// stable release.
	StateNothingToPromote = "nothing_to_promote"
	// StateNotBuilt: a commit status of the candidate's commit is not
	// success; statusState names the combined state, newestBuilt the
	// highest other candidate that is built.
	StateNotBuilt = "not_built"
	// StateNotAutoRelease: the repository has no auto-release workflow.
	StateNotAutoRelease = "not_auto_release"
	// StateOutdatedWorkflow: the auto-release workflow on the default branch
	// has no promotion step: its release-type stable tags the branch head; or,
	// with --version, its promotion step takes no version input.
	StateOutdatedWorkflow = "outdated_workflow"
	// StateFailed: a read or the dispatch failed; the message has GitHub's
	// answer.
	StateFailed = "failed"
)

// Repository is one repository's promotion as the document reports it.
type Repository struct {
	// Repository is owner/repo.
	Repository string `json:"repository"`
	// Stable is the tag of the latest stable release; empty when there is
	// none.
	Stable string `json:"stable"`
	// Candidate is the tag of the candidate to promote; empty when there is
	// none.
	Candidate string `json:"candidate"`
	// NewestBuilt is, for not_built, the highest other candidate the
	// workflow would promote whose commit is built: promote it with
	// --version instead of waiting. Empty when there is none or for another
	// state.
	NewestBuilt string `json:"newestBuilt,omitempty"`
	// Later are, with --version, the release candidates on the default
	// branch cut after the promoted one, lowest first: the stable release
	// is the promoted candidate's commit, so they are not in it. Empty
	// without --version or when there is none.
	Later []string `json:"later,omitempty"`
	// StatusState is the combined commit status of the candidate: success,
	// pending, failure or error; empty until read.
	StatusState string `json:"statusState"`
	// State is one of the State constants.
	State string `json:"state"`
	// Message says what was done or why not.
	Message string `json:"message"`
	// Warnings name what the promotion went on despite: a failed commit
	// status of an advisory job (circleciclient.AdvisoryJobs), which the
	// release does not require. Empty when there is none.
	Warnings []string `json:"warnings,omitempty"`
}

// OK says whether the repository is dispatched, would be, or has nothing to
// promote.
func (r Repository) OK() bool {
	switch r.State {
	case StateDispatched, StateWouldDispatch, StateNothingToPromote:
		return true
	}
	return false
}

func (r Repository) set(state, format string, args ...any) Repository {
	r.State = state
	r.Message = fmt.Sprintf(format, args...)
	return r
}

// Identity is the GitHub token a promotion acts with.
type Identity struct {
	// Source is "keychain", the devctl GitHub App login, or "$NAME", the
	// environment variable that overrides it; empty until the token is
	// resolved.
	Source string `json:"source"`
	// Login is the account of the App login; empty for a token from the
	// environment, whose account devctl does not look up.
	Login string `json:"login"`
}

// Result is the command's document below the envelope.
type Result struct {
	// Team is the team whose repositories were promoted; empty for named
	// repositories.
	Team string `json:"team"`
	// DryRun is set with --dry-run: nothing was dispatched.
	DryRun bool `json:"dryRun"`
	// Identity is who dispatched the workflows, or would with --dry-run.
	Identity Identity `json:"identity"`
	// Repositories are the repositories in the order they were given or
	// the team file lists them.
	Repositories []Repository `json:"repositories"`
}

// NewResult is the result before anything is known: no repositories.
func NewResult(team string) Result {
	return Result{Team: team, Repositories: []Repository{}}
}

// Document is the command's JSON: the envelope and the result.
type Document struct {
	agentcli.Envelope
	Result
}
