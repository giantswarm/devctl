package circleciapp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

// fakeCircleCI is a project with its definitions and triggers, recording
// the writes.
type fakeCircleCI struct {
	noProject bool
	// followCreates makes Follow create the project, as CircleCI does for
	// an organization repository: an OAuth definition with its trigger.
	followCreates bool
	forbidden     bool
	defs          []circleciclient.PipelineDefinition
	triggers      map[string][]circleciclient.Trigger
	// pipelines answers GetPipeline and ListPipelineWorkflows in turn, one
	// read per entry; the last repeats.
	reads  []pipelineRead
	read   int
	writes []string
	seq    int
	// setupWorkflows is the project's setting; settingsRefused answers its
	// read with 403.
	setupWorkflows  bool
	settingsRefused bool
	// failReads answers that many pipeline reads with a 502 first.
	failReads int
	// oauthJobs are the jobs an OAuth status links, by number, with the
	// pipeline each ran in: its checkout tag, or the default branch for "".
	oauthJobs map[int64]string
	// oauthWorkflows are the workflows an OAuth status links, as a follow
	// posts them, with the checkout tag of each one's pipeline ("" for the
	// default branch).
	oauthWorkflows map[string]string
	// workflowReads counts the reads of OAuth workflows, by id.
	workflowReads map[string]int
}

type pipelineRead struct {
	state     string
	workflows []circleciclient.Workflow
}

func newFake(defs ...circleciclient.PipelineDefinition) *fakeCircleCI {
	return &fakeCircleCI{defs: defs, triggers: map[string][]circleciclient.Trigger{}, setupWorkflows: true}
}

func oauthDef() circleciclient.PipelineDefinition {
	return circleciclient.PipelineDefinition{ID: "oauth", Name: "sample", ConfigSource: circleciclient.DefinitionSource{Provider: circleciclient.ProviderGitHubOAuth}}
}

func appDef() circleciclient.PipelineDefinition {
	d := circleciclient.AppDefinition("sample-app", "", "42")
	d.ID = "app"
	return d
}

func (f *fakeCircleCI) GetProject(_ context.Context, org, repo string) (*circleciclient.Project, error) {
	if f.noProject {
		return nil, notFound()
	}
	return &circleciclient.Project{ID: "p1", Slug: "gh/" + org + "/" + repo}, nil
}

func (f *fakeCircleCI) Follow(_ context.Context, org, repo string) error {
	f.writes = append(f.writes, "follow "+org+"/"+repo)
	if f.forbidden {
		return forbidden()
	}
	if f.followCreates && f.noProject {
		f.noProject = false
		f.defs = append(f.defs, oauthDef())
		f.triggers["oauth"] = append(f.triggers["oauth"], circleciclient.Trigger{ID: "followed"})
	}
	return nil
}

func (f *fakeCircleCI) ListPipelineDefinitions(context.Context, string) ([]circleciclient.PipelineDefinition, error) {
	return f.defs, nil
}

func (f *fakeCircleCI) CreatePipelineDefinition(_ context.Context, _ string, def circleciclient.PipelineDefinition) (*circleciclient.PipelineDefinition, error) {
	if f.forbidden {
		return nil, forbidden()
	}
	def.ID = "created"
	f.defs = append(f.defs, def)
	f.writes = append(f.writes, fmt.Sprintf("create definition %s repo %s file %s", def.Name, def.ConfigSource.Repo.ExternalID, def.ConfigSource.FilePath))
	return &def, nil
}

func (f *fakeCircleCI) ListTriggers(_ context.Context, _, definitionID string) ([]circleciclient.Trigger, error) {
	return f.triggers[definitionID], nil
}

func (f *fakeCircleCI) CreateTrigger(_ context.Context, _, definitionID string, t circleciclient.Trigger) (*circleciclient.Trigger, error) {
	t.ID = "t-" + definitionID
	f.triggers[definitionID] = append(f.triggers[definitionID], t)
	f.writes = append(f.writes, fmt.Sprintf("create trigger %s on %s", t.EventPreset, definitionID))
	return &t, nil
}

func (f *fakeCircleCI) DeleteTrigger(_ context.Context, _, triggerID string) error {
	for d, ts := range f.triggers {
		for i, t := range ts {
			if t.ID == triggerID {
				f.triggers[d] = append(ts[:i], ts[i+1:]...)
			}
		}
	}
	f.writes = append(f.writes, "delete trigger "+triggerID)
	return nil
}

func (f *fakeCircleCI) RunPipeline(_ context.Context, _, _, definitionID string, ref circleciclient.Ref) (*circleciclient.Pipeline, error) {
	f.seq++
	f.writes = append(f.writes, fmt.Sprintf("run %s on %s", definitionID, ref))
	return &circleciclient.Pipeline{ID: fmt.Sprintf("pl%d", f.seq), Number: int64(f.seq), State: "setup-pending"}, nil
}

func (f *fakeCircleCI) current() pipelineRead {
	if len(f.reads) == 0 {
		return pipelineRead{state: "created", workflows: []circleciclient.Workflow{{Name: "build", Status: "success"}}}
	}
	if f.read >= len(f.reads) {
		return f.reads[len(f.reads)-1]
	}
	return f.reads[f.read]
}

func (f *fakeCircleCI) GetProjectJob(_ context.Context, _, _ string, number int64) (*circleciclient.ProjectJob, error) {
	if _, ok := f.oauthJobs[number]; !ok {
		return nil, notFound()
	}
	j := &circleciclient.ProjectJob{Number: number}
	j.Pipeline.ID = fmt.Sprintf("oauth-%d", number)
	return j, nil
}

func (f *fakeCircleCI) GetPipeline(_ context.Context, id string) (*circleciclient.PipelineDetail, error) {
	tag, oauth := "", false
	var number int64
	if _, err := fmt.Sscanf(id, "oauth-%d", &number); err == nil {
		tag, oauth = f.oauthJobs[number], true
	}
	if wf, found := strings.CutPrefix(id, "oauth-wf-"); found {
		tag, oauth = f.oauthWorkflows[wf], true
	}
	if oauth {
		p := &circleciclient.PipelineDetail{Pipeline: circleciclient.Pipeline{ID: id}, ProjectSlug: circleciclient.ProjectSlug("giantswarm", "sample")}
		if tag != "" {
			p.VCS.Tag = tag
		} else {
			p.VCS.Branch = "main"
		}
		return p, nil
	}
	if f.failReads > 0 {
		f.failReads--
		return nil, clientError(502)
	}
	return &circleciclient.PipelineDetail{Pipeline: circleciclient.Pipeline{ID: id, State: f.current().state}}, nil
}

func (f *fakeCircleCI) GetWorkflow(_ context.Context, id string) (*circleciclient.WorkflowDetail, error) {
	if f.workflowReads == nil {
		f.workflowReads = map[string]int{}
	}
	f.workflowReads[id]++
	if _, ok := f.oauthWorkflows[id]; ok {
		return &circleciclient.WorkflowDetail{PipelineID: "oauth-wf-" + id, ProjectSlug: circleciclient.ProjectSlug("giantswarm", "sample")}, nil
	}
	if f.oauthWorkflows != nil {
		return nil, notFound()
	}
	return nil, fmt.Errorf("unexpected workflow attribution read")
}

func (f *fakeCircleCI) ListPipelineWorkflows(context.Context, string) ([]circleciclient.Workflow, error) {
	r := f.current()
	f.read++
	return r.workflows, nil
}

// notFound and forbidden are CircleCI's own errors, from a client against a
// server answering the status.
func notFound() error  { return clientError(404) }
func forbidden() error { return clientError(403) }

// fakeGitHub is a repository with a latest release and the statuses of its
// tag's commit.
type fakeGitHub struct {
	release  *github.RepositoryRelease
	statuses []*github.RepoStatus
	noConfig bool
	releases []*github.RepositoryRelease
	// head are the statuses of the default branch's head; statuses those
	// of every other ref (the release tags).
	head []*github.RepoStatus
	// shas maps refs to commits; every other ref has a commit of its own.
	shas map[string]string
}

func (g *fakeGitHub) Repository(context.Context, string, string) (int64, string, error) {
	return 42, "main", nil
}

func (g *fakeGitHub) HasConfig(context.Context, string, string, string) (bool, error) {
	return !g.noConfig, nil
}

func (g *fakeGitHub) CommitSHA(_ context.Context, _, _, ref string) (string, error) {
	if sha, ok := g.shas[ref]; ok {
		return sha, nil
	}
	return "sha-" + ref, nil
}

func (g *fakeGitHub) Releases(context.Context, string, string) ([]*github.RepositoryRelease, error) {
	return g.releases, nil
}

func (g *fakeGitHub) LatestRelease(context.Context, string, string) (*github.RepositoryRelease, error) {
	return g.release, nil
}

func (g *fakeGitHub) CircleCIStatuses(_ context.Context, _, _, ref string) ([]*github.RepoStatus, error) {
	if ref == "main" {
		return g.head, nil
	}
	return g.statuses, nil
}

// built gives the default branch's head a status of the App pipeline.
func (g *fakeGitHub) built() *fakeGitHub {
	g.head = []*github.RepoStatus{status("go-build", "success", "head", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))}
	return g
}

func release(tag string, published time.Time) *github.RepositoryRelease {
	return &github.RepositoryRelease{TagName: tag, PublishedAt: &github.Timestamp{Time: published}}
}

// oauthStatus is a status an OAuth follow posted as login, linking job
// number of the project.
func oauthStatus(context, state, login string, job int64, at time.Time) *github.RepoStatus {
	return &github.RepoStatus{
		Context:   new("ci/circleci: " + context),
		State:     new(state),
		TargetURL: new(fmt.Sprintf("https://circleci.com/gh/giantswarm/sample/%d", job)),
		CreatedAt: &github.Timestamp{Time: at},
		Creator:   &github.User{Login: new(login)},
	}
}

// oauthWorkflowStatus is a status an OAuth follow posted as login in the
// form a follow posts it, linking its job by workflow.
func oauthWorkflowStatus(context, state, login, workflow string, at time.Time) *github.RepoStatus {
	st := status(context, state, workflow, at)
	st.Creator = &github.User{Login: new(login)}
	return st
}

func status(context, state, workflow string, at time.Time) *github.RepoStatus {
	return &github.RepoStatus{
		Context:   new("ci/circleci: " + context),
		State:     new(state),
		TargetURL: new("https://app.circleci.com/workflow/" + workflow + "/job/j"),
		CreatedAt: &github.Timestamp{Time: at},
		Creator:   &github.User{Login: new("circleci-app[bot]")},
	}
}

func noSleep(context.Context, time.Duration) error { return nil }

func (f *fakeCircleCI) GetProjectSettings(context.Context, string, string) (*circleciclient.ProjectSettings, error) {
	if f.settingsRefused {
		return nil, forbidden()
	}
	on := f.setupWorkflows
	return &circleciclient.ProjectSettings{Advanced: circleciclient.AdvancedSettings{SetupWorkflows: &on}}, nil
}

func (f *fakeCircleCI) UpdateProjectSettings(_ context.Context, _, _ string, s circleciclient.ProjectSettings) (*circleciclient.ProjectSettings, error) {
	f.setupWorkflows = *s.Advanced.SetupWorkflows
	f.writes = append(f.writes, "enable setup workflows")
	return &s, nil
}
