package reconcile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v92/github"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

// LifecycleArchived is the lifecycle value that archives a repository.
const LifecycleArchived = "archived"

// LifecycleDeleted is the lifecycle value that deletes a repository on
// GitHub; the entry stays in the team file as the record of the deletion.
const LifecycleDeleted = "deleted"

// lifecycleOver says whether the declared lifecycle ends the repository's
// life — archived or deleted — so that the lifecycle step is the one that
// applies.
func lifecycleOver(lifecycle string) bool {
	return lifecycle == LifecycleArchived || lifecycle == LifecycleDeleted
}

// ReportedChecker returns the check contexts that have reported on the
// default branch or a recently merged pull request — the reported-only rule
// of `devctl repo checks`. *githubclient.Client satisfies it.
type ReportedChecker interface {
	ReportedChecks(ctx context.Context, repository *github.Repository, branch string) ([]string, error)
}

// ScaffoldRenderer renders the scaffold of an accepted entry into a
// directory. reposetup.Renderer satisfies it.
type ScaffoldRenderer interface {
	Render(ctx context.Context, req reposetup.RenderRequest) (*reposetup.Scaffold, error)
}

// Runner runs the set-up steps. GitHub is required; the others are optional
// and their absence skips what needs them: without CircleCI the CircleCI
// and release steps are skipped, without Checks no check is required on the
// reported-only rule (none is removed on a guess either), without Renderer
// the scaffold cannot be repaired.
type Runner struct {
	// GitHub is the client the steps read and write GitHub with, under the
	// App installation token or the person's.
	GitHub *github.Client
	// Dispatch is the client the catalog step lists and dispatches the
	// catalog and mapping workflow runs with; nil means GitHub. A caller
	// whose GitHub identity holds no Actions permission on the catalog
	// repository gives the token that does (a workflow run's own, with
	// actions: write) here and keeps every read — the repository lookup,
	// the catalog and the mapping — with GitHub, which sees the private
	// repositories that token does not.
	Dispatch *github.Client
	// Checks answers which check contexts have reported.
	Checks ReportedChecker
	// CircleCI is the client for follow, settings, keys and pipelines.
	CircleCI *circleciclient.Client
	// Renderer renders the scaffold the scaffold step pushes.
	Renderer ScaffoldRenderer
	// Baseline is the set-up applied on top of the declaration; nil means
	// [DefaultBaseline].
	Baseline *Baseline
	// Log receives one line per step and change; nil discards.
	Log io.Writer
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// Request is one run over one declared repository.
type Request struct {
	// Owner is the GitHub organization; empty means reposetup.DefaultOwner.
	Owner string
	// Team is the slug of the team file the entry lives in (team-bumblebee).
	Team string
	// Entry is an accepted entry of the front half's dry run; its Rendered
	// declaration is the desired state.
	Entry reposetup.Entry
	// Added says the triggering change added the entry: the one condition
	// under which a missing repository is created.
	Added bool
	// Mode is check or repair; empty means check.
	Mode Mode
	// Steps restricts the run to the named steps; nil runs every step. The
	// repository is looked up in any case.
	Steps []Step
	// RenderOptions selects among the template's options for the scaffold.
	RenderOptions map[string]string
	// Pipeline holds the documents of the generated CircleCI pipeline
	// (workflows.yml, custom.yml) the protection step reads the branch-side
	// jobs from instead of the repository's .circleci — the files a caller
	// has just generated and not pushed yet. Nil reads the repository.
	Pipeline [][]byte
}

// teamSteps are the steps that read the team: the scaffold names it in
// CODEOWNERS and the chart, the CODEOWNERS step writes it, the catalog
// maps the repository to it.
var teamSteps = []Step{StepScaffold, StepCodeowners, StepCatalog}

// needsTeam says whether a run over steps (nil: every step) reads the team.
func needsTeam(steps []Step) bool {
	if steps == nil {
		return true
	}
	for _, s := range steps {
		if containsStep(teamSteps, s) {
			return true
		}
	}
	return false
}

// run is the state the steps share.
type run struct {
	req      Request
	baseline Baseline
	owner    string
	// declared is the name in the team file, name the name the run
	// addresses (the renamed one after a redirect).
	declared, name string
	fields         reposetup.Fields
	repo           *github.Repository // nil when the repository does not exist
	// created says the create step created the repository in this run.
	created bool
	renamed bool
	empty   bool // no commits on the default branch
	// headSHA is the commit at the head of the default branch as the
	// scaffold step found it; scaffoldSHA the scaffold commit it pushed.
	headSHA, scaffoldSHA string
	// scaffoldFailed says the scaffold step could not push the scaffold:
	// the steps that need it on the default branch wait for the next run,
	// as they do on an empty repository — protecting the branch first
	// would keep the scaffold from ever landing.
	scaffoldFailed bool
	// pipeline says whether the repository has a CircleCI pipeline, once
	// hasPipeline has read it; nil before. The circleci and release steps
	// share the one answer.
	pipeline *bool
	log      io.Writer
}

// errReported marks a repair that ended in a finding instead of a change.
var errReported = errors.New("reported")

// Run executes the steps for req and returns the result. An error means the
// run could not start (an unaccepted entry, no GitHub client); a step that
// fails is a [VerdictFailed] in the result and the run continues.
func (r *Runner) Run(ctx context.Context, req Request) (*Result, error) {
	s, err := r.newRun(req)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	req = s.req

	res := &Result{
		Declared:  s.owner + "/" + s.declared,
		Team:      req.Team,
		Mode:      req.Mode,
		Added:     req.Added,
		StartedAt: r.now(),
	}
	res.Converged = true
	for _, step := range Steps {
		selected := req.Steps == nil || containsStep(req.Steps, step)
		if !selected && step != StepCreate {
			continue
		}
		sr := r.execute(ctx, s, step)
		if !selected {
			continue // the lookup ran for the other steps; not part of the result
		}
		res.Steps = append(res.Steps, *sr)
		if !sr.Converges() {
			res.Converged = false
		}
	}
	res.Repository = s.owner + "/" + s.name
	res.FinishedAt = r.now()
	return res, nil
}

// newRun checks what every run needs — a GitHub client, an accepted entry,
// the team where a step reads it, a known mode and known steps — fills the
// request's defaults and parses the rendered entry into the state the steps
// share. [Run] and [Create] start here.
func (r *Runner) newRun(req Request) (*run, error) {
	if r.GitHub == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.GitHub must not be empty", r)
	}
	if !req.Entry.Accepted {
		return nil, microerror.Maskf(invalidConfigError, "entry %q is not accepted: %d problems", req.Entry.Name, len(req.Entry.Problems))
	}
	if req.Team == "" && needsTeam(req.Steps) {
		return nil, microerror.Maskf(invalidConfigError, "%T.Team must not be empty: the scaffold, codeowners and catalog steps name the team", req)
	}
	switch req.Mode {
	case "":
		req.Mode = ModeCheck
	case ModeCheck, ModeRepair:
	default:
		return nil, microerror.Maskf(invalidConfigError, "%T.Mode %q: want check or repair", req, req.Mode)
	}
	for _, step := range req.Steps {
		if !knownStep(step) {
			return nil, microerror.Maskf(invalidConfigError, "unknown step %q", step)
		}
	}
	if req.Owner == "" {
		req.Owner = reposetup.DefaultOwner
	}

	tf, err := reposetup.ParseTeamFile(req.Team, strings.NewReader(req.Entry.Rendered))
	if err != nil {
		return nil, microerror.Mask(err)
	}
	if len(tf.Entries) != 1 {
		return nil, microerror.Maskf(invalidConfigError, "entry %q: Rendered holds %d entries, want 1", req.Entry.Name, len(tf.Entries))
	}
	fields, err := tf.Entries[0].Fields()
	if err != nil {
		return nil, microerror.Mask(err)
	}

	s := &run{
		req:      req,
		baseline: DefaultBaseline(),
		owner:    req.Owner,
		declared: fields.Name,
		name:     fields.Name,
		fields:   fields,
		log:      io.Discard,
	}
	if r.Baseline != nil {
		s.baseline = *r.Baseline
	}
	if r.Log != nil {
		s.log = r.Log
	}
	return s, nil
}

// execute runs one step, applying the conditions under which a step does
// not run, and returns its result.
func (r *Runner) execute(ctx context.Context, s *run, step Step) *StepResult {
	sr := &StepResult{Step: step}
	reason, err := r.skipReason(ctx, s, step)
	if err == nil {
		if reason != "" {
			sr.Verdict = VerdictSkipped
			sr.Summary = reason
			fmt.Fprintf(s.log, "%s/%s %s: skipped: %s\n", s.owner, s.name, step, reason)
			return sr
		}
		err = r.runStep(ctx, s, step, sr)
	}
	if err != nil && !errors.Is(err, errReported) {
		sr.Verdict = VerdictFailed
		sr.Summary = err.Error()
		if step == StepScaffold {
			s.scaffoldFailed = true
		}
	}
	s.finish(sr)
	fmt.Fprintf(s.log, "%s/%s %s: %s%s\n", s.owner, s.name, step, sr.Verdict, summaryLine(sr))
	return sr
}

// runStep runs one step's body into sr.
func (r *Runner) runStep(ctx context.Context, s *run, step Step, sr *StepResult) error {
	var err error
	switch step {
	case StepCreate:
		err = r.stepCreate(ctx, s, sr)
	case StepScaffold:
		err = r.stepScaffold(ctx, s, sr)
	case StepSettings:
		err = r.stepSettings(ctx, s, sr)
	case StepPermissions:
		err = r.stepPermissions(ctx, s, sr)
	case StepProtection:
		err = r.stepProtection(ctx, s, sr)
	case StepCircleCI:
		err = r.stepCircleCI(ctx, s, sr)
	case StepWebhooks:
		err = r.stepWebhooks(ctx, s, sr)
	case StepRenovate:
		err = r.stepRenovate(ctx, s, sr)
	case StepCodeowners:
		err = r.stepCodeowners(ctx, s, sr)
	case StepMetadata:
		err = r.stepMetadata(ctx, s, sr)
	case StepLifecycle:
		err = r.stepLifecycle(ctx, s, sr)
	case StepCatalog:
		err = r.stepCatalog(ctx, s, sr)
	case StepRelease:
		err = r.stepRelease(ctx, s, sr)
	}
	return err
}

// skipReason says why step does not run in the current state, or "". The
// circleci and release steps apply to a repository with a pipeline only,
// which is read from the repository once (hasPipeline).
func (r *Runner) skipReason(ctx context.Context, s *run, step Step) (string, error) {
	if step == StepCreate {
		return "", nil
	}
	if s.repo == nil {
		if s.fields.Lifecycle == LifecycleDeleted {
			return "deleted, as declared", nil
		}
		return "repository does not exist", nil
	}
	if lifecycleOver(s.fields.Lifecycle) && step != StepLifecycle {
		return "lifecycle: " + s.fields.Lifecycle, nil
	}
	if s.repo.GetArchived() && s.fields.Lifecycle != LifecycleArchived {
		switch step {
		case StepLifecycle, StepRelease:
		default:
			return "archived on GitHub", nil
		}
	}
	switch step {
	case StepProtection, StepCircleCI, StepRenovate, StepCodeowners, StepRelease:
		if s.empty {
			return "repository is empty: the scaffold comes first", nil
		}
		if s.scaffoldFailed {
			return "the scaffold step failed: the scaffold comes first", nil
		}
	}
	switch step {
	case StepCircleCI, StepRelease:
		pipeline, err := r.hasPipeline(ctx, s)
		if err != nil {
			return "", err
		}
		if !pipeline {
			return "no CircleCI pipeline", nil
		}
	}
	return "", nil
}

// plan records a change: in check mode as what a repair would do, in repair
// mode by applying it. An apply that returns errReported recorded a finding
// instead; any other error fails the step.
func (s *run) plan(sr *StepResult, change string, apply func() error) error {
	if s.req.Mode == ModeCheck {
		sr.Changes = append(sr.Changes, change)
		return nil
	}
	fmt.Fprintf(s.log, "%s/%s %s: %s\n", s.owner, s.name, sr.Step, change)
	if err := apply(); err != nil {
		if errors.Is(err, errReported) {
			return err
		}
		return fmt.Errorf("%s: %w", change, err)
	}
	sr.Changes = append(sr.Changes, change)
	return nil
}

// report adds a finding.
func (s *run) report(sr *StepResult, kind FindingKind, message, fix string) {
	sr.Findings = append(sr.Findings, newFinding(kind, message, fix))
}

// finish sets the verdict a step did not set itself.
func (s *run) finish(sr *StepResult) {
	if sr.Verdict != "" {
		return
	}
	switch {
	case len(sr.Changes) > 0 && s.req.Mode == ModeCheck:
		sr.Verdict = VerdictDrift
	case len(sr.Changes) > 0:
		sr.Verdict = VerdictRepaired
	case len(sr.Findings) > 0:
		sr.Verdict = VerdictReported
	default:
		sr.Verdict = VerdictOK
	}
}

// dispatcher is the client for the workflow-run calls of the catalog step.
func (r *Runner) dispatcher() *github.Client {
	if r.Dispatch != nil {
		return r.Dispatch
	}
	return r.GitHub
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (s *run) branch() string {
	if b := s.repo.GetDefaultBranch(); b != "" {
		return b
	}
	return s.baseline.DefaultBranch
}

// slug is owner/name as the run addresses the repository.
func (s *run) slug() string { return s.owner + "/" + s.name }

func knownStep(step Step) bool { return containsStep(Steps, step) }

func containsStep(steps []Step, step Step) bool {
	for _, s := range steps {
		if s == step {
			return true
		}
	}
	return false
}

func summaryLine(sr *StepResult) string {
	var parts []string
	if sr.Summary != "" {
		parts = append(parts, sr.Summary)
	}
	if len(sr.Changes) > 0 {
		parts = append(parts, strings.Join(sr.Changes, "; "))
	}
	for _, f := range sr.Findings {
		parts = append(parts, string(f.Kind)+": "+f.Message)
	}
	if len(parts) == 0 {
		return ""
	}
	return " — " + strings.Join(parts, " | ")
}

// isNotFound says whether a go-github call answered 404.
func isNotFound(resp *github.Response, err error) bool {
	if err == nil {
		return false
	}
	if resp != nil && resp.Response != nil && resp.StatusCode == 404 {
		return true
	}
	var ghErr *github.ErrorResponse
	return errors.As(err, &ghErr) && ghErr.Response != nil && ghErr.Response.StatusCode == 404
}

// sameSet says whether two lists hold the same names.
func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]bool, len(a))
	for _, s := range a {
		seen[s] = true
	}
	for _, s := range b {
		if !seen[s] {
			return false
		}
	}
	return true
}
