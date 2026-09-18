// Package reconcile is the back half of the repository set-up engine: every
// set-up step of a declared repository as a check and a repair, run in order
// by [Runner.Run] against GitHub and CircleCI, with the outcome as one
// structured [Result] the inventory stores.
//
// A step's check reads the repository's state and compares it with what the
// declaration and the [Baseline] want. In [ModeCheck] the step reports the
// drift as the changes it would make; in [ModeRepair] it makes them. Every
// repair is idempotent: a second run finds no drift and changes nothing.
// What the engine cannot repair it reports as a [Finding] with the fix
// spelled out: a declaration whose repository is gone, a redirect on the
// declared name (a rename the caller follows with a correction PR), a `gen
// circleci` refusal, the ABS prerequisites of a first chart build, a red
// first release, a default icon, a repository Renovate shows no sign of
// scanning.
//
// The steps, in order ([Steps]): create (only from an entry the caller marks
// as added — never from a missing repository), scaffold (rendered by the
// front half and pushed as the first commit before protection), settings
// baseline, team permissions, branch protection with the required checks on
// the reported-only rule, CircleCI (follow, setup workflows, checkout key),
// webhooks, Renovate (check only), CODEOWNERS (a pull request), description
// and visibility, lifecycle (archived → archived on GitHub and unfollowed;
// deleted → unfollowed and deleted on GitHub, the entry the record),
// catalog and mapping (the giantswarm/github workflows), first-release
// verification (tag → pipeline → workflows; a missed tag build is
// triggered).
//
// The reconciler workflow of giantswarm/github runs the steps under the App
// identity, `devctl repo reconcile` runs them as the person, `devctl repo
// setup` and `repo checks` call the same steps, and giantswarm-repo-manager
// runs the checks in [ModeCheck] to fill the inventory.
package reconcile

import "time"

// Step names one set-up step.
type Step string

// The steps, in the order [Runner.Run] executes them.
const (
	StepCreate      Step = "create"
	StepScaffold    Step = "scaffold"
	StepSettings    Step = "settings"
	StepPermissions Step = "permissions"
	StepProtection  Step = "protection"
	StepCircleCI    Step = "circleci"
	StepWebhooks    Step = "webhooks"
	StepRenovate    Step = "renovate"
	StepCodeowners  Step = "codeowners"
	StepMetadata    Step = "metadata"
	StepLifecycle   Step = "lifecycle"
	StepCatalog     Step = "catalog"
	StepRelease     Step = "release"

	// StepEntry is the declaration itself: the one step of a [Refused]
	// result, never run by [Runner.Run].
	StepEntry Step = "entry"
)

// Steps lists every step in execution order.
var Steps = []Step{
	StepCreate, StepScaffold, StepSettings, StepPermissions, StepProtection,
	StepCircleCI, StepWebhooks, StepRenovate, StepCodeowners, StepMetadata,
	StepLifecycle, StepCatalog, StepRelease,
}

// Mode is what a run does with the drift it finds.
type Mode string

const (
	// ModeCheck reports the drift as the changes a repair would make.
	ModeCheck Mode = "check"
	// ModeRepair makes the changes.
	ModeRepair Mode = "repair"
)

// Verdict is the outcome of one step.
type Verdict string

const (
	// VerdictOK: no drift, nothing to report.
	VerdictOK Verdict = "ok"
	// VerdictDrift: drift found and not repaired — the run was a check, or
	// the repair is a pull request that awaits its merge.
	VerdictDrift Verdict = "drift"
	// VerdictRepaired: drift found and repaired in this run.
	VerdictRepaired Verdict = "repaired"
	// VerdictReported: no drift the engine repairs, but findings with a fix
	// for a person.
	VerdictReported Verdict = "reported"
	// VerdictSkipped: the step did not apply (repository missing or empty,
	// archived or deleted, no client for the system).
	VerdictSkipped Verdict = "skipped"
	// VerdictFailed: the step could not run to its end; Summary says why.
	VerdictFailed Verdict = "failed"
)

// FindingKind classifies a finding.
type FindingKind string

// The kinds of finding.
const (
	// FindingRepositoryMissing: the declared repository does not exist and
	// the entry was not added by this change.
	FindingRepositoryMissing FindingKind = "repository-missing"
	// FindingRenamed: the declared name redirects to a renamed repository.
	FindingRenamed FindingKind = "renamed"
	// FindingGenCircleCIRefused: the CircleCI generator would produce no
	// jobs for the declaration.
	FindingGenCircleCIRefused FindingKind = "gen-circleci-refused"
	// FindingABSPrerequisite: the chart lacks something app-build-suite
	// validates on the first build.
	FindingABSPrerequisite FindingKind = "abs-prerequisite"
	// FindingDefaultIcon: the chart still carries the default icon.
	FindingDefaultIcon FindingKind = "default-icon"
	// FindingRedRelease: the latest release's tag pipeline failed.
	FindingRedRelease FindingKind = "red-release"
	// FindingRenovateNotScanned: the repository shows no sign that Renovate
	// scans it — no configuration, or a configuration without a trace of a
	// run (the Dependency Dashboard issue, a pull request, a commit).
	FindingRenovateNotScanned FindingKind = "renovate-not-scanned"
	// FindingArchivedUndeclared: archived on GitHub without lifecycle:
	// archived.
	FindingArchivedUndeclared FindingKind = "archived-undeclared"
	// FindingPendingPullRequest: a repair landed as a pull request that
	// awaits its merge.
	FindingPendingPullRequest FindingKind = "pending-pull-request"
	// FindingUnchecked: a check could not run with the caller's access.
	FindingUnchecked FindingKind = "unchecked"
	// FindingEntryRefused: the validator refused the entry for a reason
	// other than the CircleCI generator; the fix names the field.
	FindingEntryRefused FindingKind = "entry-refused"
)

// Finding is something a step reports for a person, with the fix.
type Finding struct {
	Kind    FindingKind `json:"kind"`
	Message string      `json:"message"`
	Fix     string      `json:"fix"`
}

// StepResult is the outcome of one step.
type StepResult struct {
	Step    Step    `json:"step"`
	Verdict Verdict `json:"verdict"`
	// Summary is one line on the state found.
	Summary string `json:"summary,omitempty"`
	// Changes are the repairs made ([ModeRepair]) or the repairs a run
	// would make ([ModeCheck]).
	Changes []string `json:"changes,omitempty"`
	// Findings are reported, not repaired; each carries its fix.
	Findings []Finding `json:"findings,omitempty"`
}

// Result is the outcome of one run over one repository: the structured
// value the inventory stores and the callers render.
type Result struct {
	// Repository is owner/name as the run addressed it — the renamed name
	// when the declared one redirected.
	Repository string `json:"repository"`
	// Declared is owner/name as the team file declares it.
	Declared string `json:"declared"`
	Team     string `json:"team"`
	Mode     Mode   `json:"mode"`
	// Added says whether the entry was passed as added by the triggering
	// change, which alone allows the create step to create.
	Added      bool         `json:"added"`
	StartedAt  time.Time    `json:"startedAt"`
	FinishedAt time.Time    `json:"finishedAt"`
	Steps      []StepResult `json:"steps"`
	// Converged says no step ended in drift or failure: the repository is
	// set up as declared (findings for a person may remain).
	Converged bool `json:"converged"`
}

// Step returns the result of step, or nil when the run did not execute it.
func (r *Result) Step(step Step) *StepResult {
	for i := range r.Steps {
		if r.Steps[i].Step == step {
			return &r.Steps[i]
		}
	}
	return nil
}

// Findings returns every finding of the run, in step order.
func (r *Result) Findings() []Finding {
	var all []Finding
	for _, s := range r.Steps {
		all = append(all, s.Findings...)
	}
	return all
}
