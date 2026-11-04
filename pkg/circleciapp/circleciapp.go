// Package circleciapp sets a repository up on a CircleCI GitHub App
// pipeline, as the person running devctl: a pipeline definition reading
// .circleci/config.yml through the CircleCI GitHub App, an all-pushes
// trigger on it, and optionally the OAuth pipeline's triggers deleted.
//
// CircleCI has no service accounts, so the writes are the person's, with
// their devctl CircleCI login (granted Write). Afterwards nobody is
// load-bearing: checkout, events and statuses go through the App's
// installation, and the pipeline keeps building when its creator leaves.
// That is what an OAuth follow cannot do, whose builds run through the
// follower's GitHub grant.
package circleciapp

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

// CircleCI is what Ensure and Wait ask of CircleCI, as the person.
type CircleCI interface {
	GetProject(ctx context.Context, org, repo string) (*circleciclient.Project, error)
	ListPipelineDefinitions(ctx context.Context, projectID string) ([]circleciclient.PipelineDefinition, error)
	CreatePipelineDefinition(ctx context.Context, projectID string, def circleciclient.PipelineDefinition) (*circleciclient.PipelineDefinition, error)
	ListTriggers(ctx context.Context, projectID, definitionID string) ([]circleciclient.Trigger, error)
	CreateTrigger(ctx context.Context, projectID, definitionID string, trigger circleciclient.Trigger) (*circleciclient.Trigger, error)
	DeleteTrigger(ctx context.Context, projectID, triggerID string) error
	RunPipeline(ctx context.Context, org, repo, definitionID string, ref circleciclient.Ref) (*circleciclient.Pipeline, error)
	GetPipeline(ctx context.Context, id string) (*circleciclient.PipelineDetail, error)
	GetWorkflow(ctx context.Context, id string) (*circleciclient.WorkflowDetail, error)
	GetProjectSettings(ctx context.Context, org, repo string) (*circleciclient.ProjectSettings, error)
	UpdateProjectSettings(ctx context.Context, org, repo string, settings circleciclient.ProjectSettings) (*circleciclient.ProjectSettings, error)
	ListPipelineWorkflows(ctx context.Context, pipelineID string) ([]circleciclient.Workflow, error)
}

// Repository is the GitHub repository to set up: its owner and name, and
// GitHub's id of it, which CircleCI names the repository by.
type Repository struct {
	Owner, Name string
	ID          int64
}

// Slug is owner/name.
func (r Repository) Slug() string { return r.Owner + "/" + r.Name }

// DefinitionName is the name of the GitHub App pipeline Ensure creates,
// <repository>-app: what the CircleCI UI suggests, and what the pipelines
// set up by hand on 2026-10-22 and 2026-11-02 are named.
func DefinitionName(repo string) string { return repo + "-app" }

// Result is what Ensure found and did.
type Result struct {
	ProjectID string
	// Definition is the GitHub App pipeline, found or created.
	Definition circleciclient.PipelineDefinition
	// CreatedDefinition and CreatedTrigger say Ensure created them.
	CreatedDefinition, CreatedTrigger bool
	// OAuthTriggers are the enabled triggers of the OAuth pipeline left in
	// place (without migrate): every push builds on both pipelines.
	OAuthTriggers []string
	// DeletedOAuthTriggers are the OAuth pipeline's triggers migrate
	// deleted.
	DeletedOAuthTriggers []string
	// EnabledSetupWorkflows says Ensure switched the project's setup
	// workflows on.
	EnabledSetupWorkflows bool
	// SetupWorkflowsUnread is why the setting could not be read or written,
	// empty when it was: CircleCI may refuse the person its project
	// settings, and a project created since late 2023 has them on anyway.
	SetupWorkflowsUnread string
}

// Changed says Ensure wrote anything.
func (r *Result) Changed() bool {
	return r.CreatedDefinition || r.CreatedTrigger || len(r.DeletedOAuthTriggers) > 0 || r.EnabledSetupWorkflows
}

// Ensure makes repo build on a GitHub App pipeline, idempotently: it finds
// the project's GitHub App definition or creates DefinitionName(repo), and
// gives it an all-pushes trigger unless it has an enabled trigger already.
// With migrate the OAuth pipeline's triggers are deleted after, so a push
// builds once, on the App; the OAuth definition itself is CircleCI's and
// stays. The project's setup workflows, which the generated dynamic
// configuration needs, are switched on when they are off; CircleCI refusing
// the person the settings (403) is noted in the result, not an error. A login
// that granted Read access only is refused with circleciclient.IsForbidden on
// the first write.
func Ensure(ctx context.Context, ci CircleCI, repo Repository, migrate bool) (*Result, error) {
	project, err := ci.GetProject(ctx, repo.Owner, repo.Name)
	switch {
	case circleciclient.IsNotFound(err):
		return nil, microerror.Maskf(projectNotFoundError, "CircleCI knows no project %s: an organization repository has one once CircleCI saw the organization's GitHub App installation, so check that the CircleCI GitHub App is installed on it", repo.Slug())
	case err != nil:
		return nil, microerror.Mask(err)
	}
	result := &Result{ProjectID: project.ID}
	defs, err := ci.ListPipelineDefinitions(ctx, project.ID)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	// The App definition that is triggered already is the pipeline, as the
	// reconciler's circleci step reads it; an untriggered one is taken only
	// when none is, so a dormant definition never gets a second trigger.
	var app, dormant *circleciclient.PipelineDefinition
	var oauth []circleciclient.PipelineDefinition
	triggered := map[string]bool{}
	for i, d := range defs {
		if !d.GitHubApp() {
			oauth = append(oauth, d)
			continue
		}
		triggers, err := ci.ListTriggers(ctx, project.ID, d.ID)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		triggered[d.ID] = hasEnabled(triggers)
		switch {
		case triggered[d.ID] && app == nil:
			app = &defs[i]
		case !triggered[d.ID] && dormant == nil:
			dormant = &defs[i]
		}
	}
	if app == nil {
		app = dormant
	}
	if app == nil {
		id := strconv.FormatInt(repo.ID, 10)
		def := circleciclient.AppDefinition(DefinitionName(repo.Name), "GitHub App pipeline, created by devctl repo follow-circleci", id)
		app, err = ci.CreatePipelineDefinition(ctx, project.ID, def)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		result.CreatedDefinition = true
	}
	result.Definition = *app

	if !triggered[app.ID] {
		// The trigger watches the repository the pipeline checks out: a
		// definition reading its configuration from another repository
		// still builds this one.
		repoID := app.CheckoutSource.Repo.ExternalID
		if repoID == "" {
			repoID = strconv.FormatInt(repo.ID, 10)
		}
		if _, err := ci.CreateTrigger(ctx, project.ID, app.ID, circleciclient.AllPushesTrigger(repoID)); err != nil {
			return nil, microerror.Mask(err)
		}
		result.CreatedTrigger = true
	}

	if err := ensureSetupWorkflows(ctx, ci, repo, result); err != nil {
		return nil, microerror.Mask(err)
	}

	for _, d := range oauth {
		triggers, err := ci.ListTriggers(ctx, project.ID, d.ID)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		for _, t := range triggers {
			if !migrate {
				if !t.Disabled {
					result.OAuthTriggers = append(result.OAuthTriggers, t.ID)
				}
				continue
			}
			if err := ci.DeleteTrigger(ctx, project.ID, t.ID); err != nil {
				return nil, microerror.Mask(err)
			}
			result.DeletedOAuthTriggers = append(result.DeletedOAuthTriggers, t.ID)
		}
	}
	return result, nil
}

// ensureSetupWorkflows switches the project's setup workflows on when they
// are off. A refusal (403, or 404 for settings the person may not see) is
// noted in result.SetupWorkflowsUnread.
func ensureSetupWorkflows(ctx context.Context, ci CircleCI, repo Repository, result *Result) error {
	settings, err := ci.GetProjectSettings(ctx, repo.Owner, repo.Name)
	switch {
	case circleciclient.IsForbidden(err) || circleciclient.IsNotFound(err):
		result.SetupWorkflowsUnread = err.Error()
		return nil
	case err != nil:
		return microerror.Mask(err)
	}
	if sw := settings.Advanced.SetupWorkflows; sw != nil && *sw {
		return nil
	}
	on := true
	_, err = ci.UpdateProjectSettings(ctx, repo.Owner, repo.Name, circleciclient.ProjectSettings{Advanced: circleciclient.AdvancedSettings{SetupWorkflows: &on}})
	switch {
	case circleciclient.IsForbidden(err) || circleciclient.IsNotFound(err):
		result.SetupWorkflowsUnread = err.Error()
		return nil
	case err != nil:
		return microerror.Mask(err)
	}
	result.EnabledSetupWorkflows = true
	return nil
}

func hasEnabled(triggers []circleciclient.Trigger) bool {
	for _, t := range triggers {
		if !t.Disabled {
			return true
		}
	}
	return false
}

// Outcome is how a pipeline Wait followed ended.
type Outcome struct {
	Pipeline circleciclient.Pipeline
	// Green says every workflow succeeded; Failed names the ones that did
	// not, with their status.
	Green  bool
	Failed []string
	// OnHold names the workflows waiting for an approval: settled as far as
	// the wait goes, a person approves them in the CircleCI UI.
	OnHold []string
}

// Waiter follows one pipeline until it settles.
type Waiter struct {
	CircleCI CircleCI
	// Sleep waits between reads; Interval is how long.
	Sleep    func(ctx context.Context, d time.Duration) error
	Interval time.Duration
}

// setupOnlyReads is how many reads in a row a settled pipeline that lists its
// setup workflow alone must answer so before Wait takes it as finished: a
// continuation may create no workflow (a branch every job's filter
// ignores), and CircleCI's listing may lag behind the ones it created.
const setupOnlyReads = 3

// Wait reads the pipeline of id until its workflows are finished: a setup
// pipeline first runs its setup workflow, then the continuation creates the
// workflows that carry the build. The newest run of every workflow name
// counts. The caller's context bounds the wait.
func (w Waiter) Wait(ctx context.Context, id string) (*Outcome, error) {
	setupOnly, failedReads := 0, 0
	for {
		p, workflows, err := w.read(ctx, id)
		if err != nil {
			// A CircleCI hiccup (a 5xx, a rate limit) is read again; only
			// readFailures in a row, or the context ending, end the wait.
			if failedReads++; failedReads >= readFailures || ctx.Err() != nil {
				return nil, microerror.Mask(err)
			}
			if err := w.Sleep(ctx, w.Interval); err != nil {
				return nil, microerror.Mask(err)
			}
			continue
		}
		failedReads = 0
		if p.State == "errored" {
			return &Outcome{Pipeline: p.Pipeline, Failed: []string{"pipeline (errored: its configuration did not compile)"}}, nil
		}
		workflows = circleciclient.NewestWorkflows(workflows)
		outcome := finished(p.Pipeline, workflows)
		switch {
		case len(workflows) == 0 && !circleciclient.PipelineContinuing(p.State):
			// A settled pipeline without a workflow: its filters run
			// nothing on this ref, or the listing lags behind.
			if setupOnly++; setupOnly >= setupOnlyReads {
				return &Outcome{Pipeline: p.Pipeline, Green: true}, nil
			}
		case outcome == nil:
			setupOnly = 0
		case outcome.Green && circleciclient.SetupOnly(workflows):
			if circleciclient.PipelineContinuing(p.State) {
				setupOnly = 0
				break
			}
			if setupOnly++; setupOnly >= setupOnlyReads {
				return outcome, nil
			}
		default:
			return outcome, nil
		}
		if err := w.Sleep(ctx, w.Interval); err != nil {
			return nil, microerror.Mask(err)
		}
	}
}

// The workflow states that end the wait without a verdict of their own: a
// workflow waiting for an approval, and one CircleCI did not run.
const (
	workflowOnHold = "on_hold"
	workflowNotRun = "not_run"
)

// readFailures is how many failed reads in a row end Wait.
const readFailures = 3

// read is one read of the pipeline and its workflows.
func (w Waiter) read(ctx context.Context, id string) (*circleciclient.PipelineDetail, []circleciclient.Workflow, error) {
	p, err := w.CircleCI.GetPipeline(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	workflows, err := w.CircleCI.ListPipelineWorkflows(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	return p, workflows, nil
}

// finished is the outcome of a pipeline whose listed workflows are all
// finished, on hold or not run, nil while one runs or none is listed yet. A
// setup workflow that failed is a red outcome: nothing continues it.
func finished(p circleciclient.Pipeline, workflows []circleciclient.Workflow) *Outcome {
	if len(workflows) == 0 {
		return nil
	}
	outcome := &Outcome{Pipeline: p, Green: true}
	for _, wf := range workflows {
		switch {
		case wf.Status == workflowOnHold:
			outcome.OnHold = append(outcome.OnHold, wf.Name)
		case wf.Status == workflowNotRun:
		case !circleciclient.WorkflowFinished(wf.Status):
			return nil
		case circleciclient.WorkflowFailed(wf.Status):
			outcome.Green = false
			outcome.Failed = append(outcome.Failed, fmt.Sprintf("%s (%s)", wf.Name, wf.Status))
		}
	}
	return outcome
}
