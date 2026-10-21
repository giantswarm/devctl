// Package releasewaittest are in-process test doubles of what a release wait
// reads: GitHub, CircleCI, the registries, the team files and the catalog
// index, answering from state a test sets.
package releasewaittest

import (
	"context"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

// sequence answers the i-th call of a read with its i-th state, the last
// one once the calls run past them.
type sequence struct {
	mu    sync.Mutex
	calls map[string]int
}

func (s *sequence) next(read string, n int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.calls == nil {
		s.calls = map[string]int{}
	}
	i := s.calls[read]
	s.calls[read]++
	return min(i, max(n-1, 0))
}

// GitHub is a [releasewait.GitHub] over one repository.
type GitHub struct {
	// Tags are the repository's tags and the commit each points at.
	Tags map[string]string
	// Private makes the repository private.
	Private bool
	// Files are the repository's files at every ref, by path; a directory
	// lists the files and directories below it.
	Files map[string]string
	// Releases are the repository's GitHub releases.
	Releases []githubclient.Release
	// Merges are the pull requests' merge states by number.
	Merges map[int]githubclient.PullRequestMerge
	// WorkflowRuns are the Actions runs of a commit, one list per read.
	WorkflowRuns [][]githubclient.WorkflowRun
	// CheckRuns are the check runs of a commit, one list per read.
	CheckRuns [][]*github.CheckRun
	// Statuses are the commit statuses of a commit, newest first.
	Statuses []githubclient.CommitStatus
	// StatusReads, when set, are the commit statuses of a commit, newest
	// first, one list per read instead of Statuses.
	StatusReads [][]githubclient.CommitStatus

	reads sequence
}

var _ releasewait.GitHub = &GitHub{}

func (g *GitHub) GetPullRequestMerge(_ context.Context, owner, repo string, number int) (githubclient.PullRequestMerge, error) {
	m, ok := g.Merges[number]
	if !ok {
		return githubclient.PullRequestMerge{}, githubclient.NotFound("pull request %s/%s#%d", owner, repo, number)
	}
	return m, nil
}

func (g *GitHub) GetTagSHA(_ context.Context, owner, repo, tag string) (string, error) {
	sha, ok := g.Tags[tag]
	if !ok {
		return "", githubclient.NotFound("tag %s of %s/%s", tag, owner, repo)
	}
	return sha, nil
}

func (g *GitHub) FindTagsForCommit(_ context.Context, _, _, sha string) ([]string, error) {
	var tags []string
	for tag, at := range g.Tags {
		if at == sha {
			tags = append(tags, tag)
		}
	}
	slices.Sort(tags)
	return tags, nil
}

func (g *GitHub) CommitTags(context.Context, string, string) (map[string][]string, error) {
	out := map[string][]string{}
	for tag, sha := range g.Tags {
		out[sha] = append(out[sha], tag)
	}
	return out, nil
}

func (g *GitHub) CommitsAfter(context.Context, string, string, string, string) ([]string, error) {
	return nil, nil
}

func (g *GitHub) GetReleaseByTag(_ context.Context, owner, repo, tag string) (githubclient.Release, error) {
	for _, r := range g.Releases {
		if r.Tag == tag {
			return r, nil
		}
	}
	return githubclient.Release{}, githubclient.NotFound("release %s in %s/%s", tag, owner, repo)
}

func (g *GitHub) ListReleases(context.Context, string, string) ([]githubclient.Release, error) {
	return g.Releases, nil
}

func (g *GitHub) Reachable(context.Context, string, string, string, string) (bool, error) {
	return true, nil
}

func (g *GitHub) CheckRunsForRef(context.Context, string, string, string) ([]*github.CheckRun, error) {
	if len(g.CheckRuns) == 0 {
		return nil, nil
	}
	return g.CheckRuns[g.reads.next("check-runs", len(g.CheckRuns))], nil
}

func (g *GitHub) ListWorkflowRunsForSHA(context.Context, string, string, string) ([]githubclient.WorkflowRun, error) {
	if len(g.WorkflowRuns) == 0 {
		return nil, nil
	}
	return g.WorkflowRuns[g.reads.next("workflow-runs", len(g.WorkflowRuns))], nil
}

func (g *GitHub) ListCommitStatuses(context.Context, string, string, string) ([]githubclient.CommitStatus, error) {
	if len(g.StatusReads) > 0 {
		return g.StatusReads[g.reads.next("statuses", len(g.StatusReads))], nil
	}
	return g.Statuses, nil
}

func (g *GitHub) ListWorkflowRunJobs(context.Context, string, string, int64) ([]githubclient.WorkflowJob, error) {
	return nil, nil
}

func (g *GitHub) JobLog(context.Context, string, string, int64) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (g *GitHub) ListDirectory(_ context.Context, owner, repo, dir, ref string) ([]string, error) {
	dir = strings.Trim(dir, "/")
	var names []string
	for file := range g.Files {
		rest := file
		if dir != "" {
			var ok bool
			if rest, ok = strings.CutPrefix(file, dir+"/"); !ok {
				continue
			}
		}
		name, _, _ := strings.Cut(rest, "/")
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	if names == nil {
		return nil, githubclient.NotFound("directory %s of %s/%s at %s", dir, owner, repo, ref)
	}
	slices.Sort(names)
	return names, nil
}

func (g *GitHub) GetFile(_ context.Context, owner, repo, file, ref string) (githubclient.RepositoryFile, error) {
	content, ok := g.Files[path.Clean(file)]
	if !ok {
		return githubclient.RepositoryFile{}, githubclient.NotFound("file %s of %s/%s at %s", file, owner, repo, ref)
	}
	return githubclient.RepositoryFile{Data: []byte(content), Path: file}, nil
}

func (g *GitHub) IsPrivateRepository(context.Context, string, string) (bool, error) {
	return g.Private, nil
}

// Registry is a [releasewait.Prober]: an artifact is found once its
// reference has been probed as many times as Missing names for it.
type Registry struct {
	// Missing is, per artifact reference, how many probes find it missing
	// before it is there; a reference not named is there at once.
	Missing map[string]int
	// Never are references no probe finds.
	Never []string

	mu     sync.Mutex
	probes map[string]int
}

var _ releasewait.Prober = &Registry{}

func (r *Registry) Probe(_ context.Context, artifact releasewait.Artifact) (string, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.probes == nil {
		r.probes = map[string]int{}
	}
	ref := artifact.Reference
	r.probes[ref]++
	if slices.Contains(r.Never, ref) || r.probes[ref] <= r.Missing[ref] {
		return "", false, nil
	}
	return "sha256:" + strings.Repeat("0", 64), true, nil
}

// Probes is how often reference was probed.
func (r *Registry) Probes(reference string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.probes[reference]
}

// Entries is a [releasewait.EntryFinder] declaring every repository with
// Fields; nil Fields declares none.
type Entries struct{ Fields *reposetup.Fields }

func (e Entries) FindEntry(context.Context, string, string) (*reposetup.Fields, bool, error) {
	return e.Fields, e.Fields != nil, nil
}

// Catalog is a [releasewait.CatalogReader] that lists every chart when
// Listed is set.
type Catalog struct{ Listed bool }

func (c Catalog) Lists(context.Context, string, string, string) (bool, error) {
	return c.Listed, nil
}

// CircleCI is a [releasewait.CircleCI] with one pipeline of the tag.
type CircleCI struct {
	// Pipeline is the tag's pipeline.
	Pipeline circleciclient.Pipeline
	// Workflows are the pipeline's workflows, one list per read.
	Workflows [][]circleciclient.Workflow
	// Jobs are each workflow's jobs by its id.
	Jobs map[string][]circleciclient.Job

	reads sequence
}

var _ releasewait.CircleCI = &CircleCI{}

func (c *CircleCI) FindPipelineByTag(context.Context, string, string, string) (*circleciclient.Pipeline, error) {
	p := c.Pipeline
	return &p, nil
}

func (c *CircleCI) ListPipelineWorkflows(context.Context, string) ([]circleciclient.Workflow, error) {
	if len(c.Workflows) == 0 {
		return nil, nil
	}
	return c.Workflows[c.reads.next("workflows", len(c.Workflows))], nil
}

func (c *CircleCI) ListWorkflowJobs(_ context.Context, workflowID string) ([]circleciclient.Job, error) {
	return c.Jobs[workflowID], nil
}

func (c *CircleCI) GetProjectJob(_ context.Context, org, repo string, number int64) (*circleciclient.ProjectJob, error) {
	return nil, fmt.Errorf("no job %d of %s/%s", number, org, repo)
}

func (c *CircleCI) GetPipeline(context.Context, string) (*circleciclient.PipelineDetail, error) {
	return &circleciclient.PipelineDetail{Pipeline: c.Pipeline}, nil
}

func (c *CircleCI) GetProjectPipeline(context.Context, string, string, int64) (*circleciclient.PipelineDetail, error) {
	return &circleciclient.PipelineDetail{Pipeline: c.Pipeline}, nil
}

func (c *CircleCI) FailedStepsOutput(context.Context, string, string, int64) (string, error) {
	return "", nil
}

// jobType is CircleCI's type of a job that runs steps.
const jobType = "build"

// ReleaseSHA is the commit of the release GeneratedRelease builds.
const ReleaseSHA = "0123456789abcdef0123456789abcdef01234567"

// GeneratedRelease are the sources of release v1.2.3 of owner/repo, a
// repository whose CI devctl generates: its tag pipeline on CircleCI, one
// workflow whose status at each read is the next of workflows, pushes the
// image owner/repo and the chart repo to registry. Its one tag job,
// push-to-registries-release, posts the commit status of the workflow's
// status at each read, as CircleCI does on GitHub.
func GeneratedRelease(owner, repo string, registry *Registry, workflows ...string) (*releasewait.Sources, error) {
	teamFile, err := reposetup.ParseTeamFile("team-test", strings.NewReader(fmt.Sprintf(
		"- name: %s\n  gen:\n    flavours: [app]\n    language: go\n    ci:\n      generate: true\n      image:\n        name: %s/%s\n", repo, owner, repo)))
	if err != nil {
		return nil, err
	}
	entry, err := teamFile.Entries[0].Fields()
	if err != nil {
		return nil, err
	}
	gh := &GitHub{
		Tags: map[string]string{"v1.2.3": ReleaseSHA},
		Files: map[string]string{
			"Dockerfile":                                       "FROM scratch\n",
			"helm/" + repo + "/Chart.yaml":                     "apiVersion: v2\nname: " + repo + "\nversion: [[ .Version ]]\n",
			".circleci/config.yml":                             generatedSetup,
			".circleci/workflows.yml":                          generatedWorkflows,
			".github/workflows/zz_generated.auto_release.yaml": "on: push\n",
		},
	}
	circleci := &CircleCI{Pipeline: circleciclient.Pipeline{ID: "p1", Number: 12, VCS: circleciclient.PipelineVCS{Tag: "v1.2.3", Revision: ReleaseSHA}}}
	created := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	for _, status := range workflows {
		circleci.Workflows = append(circleci.Workflows, []circleciclient.Workflow{{ID: "w1", Name: "build", Status: status, PipelineNumber: 12, CreatedAt: created}})
	}
	for _, status := range workflows {
		gh.StatusReads = append(gh.StatusReads, []githubclient.CommitStatus{{
			Context:   circleciclient.StatusContextPrefix + ": push-to-registries-release",
			State:     commitStatusOf[status],
			TargetURL: "https://circleci.com/gh/" + owner + "/" + repo + "/101",
		}, {
			Context:   circleciclient.StatusContextPrefix + ": setup",
			State:     success,
			TargetURL: "https://circleci.com/gh/" + owner + "/" + repo + "/100",
		}})
	}
	jobs := "running"
	if len(workflows) > 0 {
		jobs = workflows[len(workflows)-1]
	}
	circleci.Jobs = map[string][]circleciclient.Job{"w1": {
		{Name: "push-to-registries-release", Status: jobs, Type: jobType},
		{Name: "push-chart-release", Status: jobs, Type: jobType},
	}}
	return &releasewait.Sources{
		GitHub:       gh,
		Entries:      Entries{Fields: &entry},
		CircleCI:     func(context.Context) (releasewait.CircleCI, error) { return circleci, nil },
		Registry:     registry,
		CatalogIndex: Catalog{},
	}, nil
}

// commitStatusOf is the commit status CircleCI posts for a job in a
// workflow's status.
var commitStatusOf = map[string]string{success: success, "failed": "failure", "running": "pending"}

// success is a successful workflow's and commit status's state.
const success = "success"

// generatedSetup and generatedWorkflows are a generated configuration's
// setup workflow and the workflows it continues with: one job on tags.
const (
	generatedSetup = `version: 2.1
setup: true
jobs:
  setup:
    steps:
    - run: cp .circleci/workflows.yml .circleci/continue.yml
workflows:
  setup:
    jobs:
    - setup:
        filters:
          tags:
            only: /^v.*/
`
	generatedWorkflows = `version: 2.1
workflows:
  build:
    jobs:
    - architect/push-to-registries:
        name: push-to-registries-release
        filters:
          tags:
            only: /^v.*/
          branches:
            ignore: /.*/
`
)
