// Package releasewait waits until a tag's images and charts are pullable:
// what `devctl release wait` does. A tag and a GitHub Release exist about a
// minute after a merge; the artifacts come from the CircleCI pipeline the
// tag triggers, minutes later, under names the repository's CI decides. The
// package reads those names from the sources that define them and never
// from the repository name: for generated CI from the team-file entry the
// generator renders (the `image.name` override included), for hand-written
// CI from the push jobs of the tag pipeline matched with the tag's CircleCI
// configuration. When the sources disagree, or yield nothing while a
// Dockerfile exists at the tag, the wait ends with the disagreement instead
// of a guess.
//
// Availability is a digest and a green tag pipeline: every image and chart
// resolves to one in the registry, the public registry probed anonymously (a
// stale docker login cannot produce a false UNAUTHORIZED), the private one
// with the docker keychain, and every workflow of the tag pipeline, the
// newest run per workflow name, finished green. The names cover what devctl
// renders or the orb pushes, not a repository's own tag jobs, so the
// pipeline is what says the release is complete. A failed or cancelled
// workflow ends the wait as the tag's CI failure with the failed jobs; a
// repository without CircleCI is judged by the Actions runs the tag
// triggered. A repository without image and chart is waited for through its
// published release and the tag's workflows.
package releasewait

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

// Command is the envelope's command name.
const Command = "release wait"

// The poll interval's bounds at scale 1; between them it follows GitHub's
// rate-limit headers.
const (
	IntervalFloor   = 15 * time.Second
	IntervalCeiling = 60 * time.Second
)

// DefaultTimeout bounds a wait that gets no --timeout.
const DefaultTimeout = 30 * time.Minute

// GitHub is what the wait reads from GitHub; *githubclient.Client is one.
type GitHub interface {
	GetPullRequestMerge(ctx context.Context, owner, repo string, number int) (githubclient.PullRequestMerge, error)
	GetTagSHA(ctx context.Context, owner, repo, tag string) (string, error)
	FindTagForCommit(ctx context.Context, owner, repo, sha string) (string, error)
	GetReleaseByTag(ctx context.Context, owner, repo, tag string) (githubclient.Release, error)
	ListWorkflowRunsForSHA(ctx context.Context, owner, repo, sha string) ([]githubclient.WorkflowRun, error)
	ListDirectory(ctx context.Context, owner, repo, path, ref string) ([]string, error)
	GetFile(ctx context.Context, owner, repo, path, ref string) (githubclient.RepositoryFile, error)
	IsPrivateRepository(ctx context.Context, owner, repo string) (bool, error)
}

// CircleCI is what the wait reads from CircleCI; *circleciclient.Client is
// one.
type CircleCI interface {
	FindPipelineByTag(ctx context.Context, org, repo, tag string) (*circleciclient.Pipeline, error)
	ListPipelineWorkflows(ctx context.Context, pipelineID string) ([]circleciclient.Workflow, error)
	ListWorkflowJobs(ctx context.Context, workflowID string) ([]circleciclient.Job, error)
}

// EntryFinder returns the team-file entry that declares a repository, and
// false when no team file does.
type EntryFinder interface {
	FindEntry(ctx context.Context, owner, repo string) (*reposetup.Fields, bool, error)
}

// Prober answers whether an artifact is in its registry: the digest when it
// is, found false when the registry knows no such manifest, and an error for
// every other answer, which is a tooling failure and not a slow pipeline.
type Prober interface {
	Probe(ctx context.Context, artifact Artifact) (digest string, found bool, err error)
}

// CatalogReader says whether a catalog's index lists a chart version.
type CatalogReader interface {
	Lists(ctx context.Context, catalog, chart, version string) (bool, error)
}

// Config configures a Waiter.
type Config struct {
	// Owner and Repo name the repository. Required.
	Owner, Repo string
	// Version is the release to wait for, vX.Y.Z or X.Y.Z; empty with PR.
	Version string
	// PR is the merged pull request whose tag is waited for; 0 with Version.
	PR int
	// Timeout bounds the wait; zero means DefaultTimeout.
	Timeout time.Duration
	// Catalog also waits for the catalog index to list every chart.
	Catalog bool

	// GitHub is required. Entries and Registry are required; CircleCI is
	// called once when the tag carries a .circleci/config.yml, so a
	// repository without CircleCI needs no CircleCI token. CatalogIndex is
	// required with Catalog.
	GitHub       GitHub
	Entries      EntryFinder
	CircleCI     func(ctx context.Context) (CircleCI, error)
	Registry     Prober
	CatalogIndex CatalogReader

	// Endpoints name the registries. Zero means production.
	Endpoints agentcli.Endpoints
	// Clock paces the polls; zero means the wall clock at scale 1.
	Clock agentcli.Clock
	// Rate is the rate limit of the last GitHub response, the interval's
	// input; nil keeps the floor.
	Rate func() githubclient.RateLimit
	// Progress receives one line per step; nil is silent.
	Progress *agentcli.Progress
	// Warn receives the warnings the document carries beside the result: a
	// team-file entry whose release model lags the repository's workflows.
	// Nil drops them.
	Warn func(message string)
}

// Waiter runs one wait.
type Waiter struct {
	config   Config
	clock    agentcli.Clock
	progress *agentcli.Progress
	circleci CircleCI
}

// New validates config and returns a Waiter.
func New(config Config) (*Waiter, error) {
	if config.Owner == "" || config.Repo == "" {
		return nil, usageErr("the repository is required as owner/repo")
	}
	if (config.Version == "") == (config.PR == 0) {
		return nil, usageErr("exactly one of a version and --pr is required")
	}
	if config.Version != "" {
		if _, err := ParseVersion(config.Version); err != nil {
			return nil, err
		}
	}
	if config.GitHub == nil || config.Entries == nil || config.Registry == nil || config.CircleCI == nil {
		return nil, usageErr("%T.GitHub, Entries, CircleCI and Registry must not be empty", config)
	}
	if config.Catalog && config.CatalogIndex == nil {
		return nil, usageErr("%T.CatalogIndex must not be empty with Catalog", config)
	}
	if config.Timeout <= 0 {
		config.Timeout = DefaultTimeout
	}
	if config.Endpoints == (agentcli.Endpoints{}) {
		config.Endpoints = agentcli.DefaultEndpoints()
	}
	if config.Progress == nil {
		config.Progress = agentcli.NewProgress(nil, false)
	}
	return &Waiter{config: config, clock: config.Clock, progress: config.Progress}, nil
}

// Wait runs the wait and fills result as it learns; the error is the
// outcome of the exit-code table, nil when every artifact is available.
// result is partially filled when the error is not nil: what was known when
// the wait ended.
func (w *Waiter) Wait(ctx context.Context, result *Result) error {
	*result = NewResult(w.config.Owner + "/" + w.config.Repo)
	ctx, cancel := w.clock.Timeout(ctx, w.config.Timeout)
	defer cancel()

	err := w.wait(ctx, result)
	if err != nil && ctx.Err() != nil && !isOutcome(err) {
		return w.timeout(result)
	}
	return err
}

func (w *Waiter) wait(ctx context.Context, result *Result) error {
	owner, repo := w.config.Owner, w.config.Repo

	// The release model decides how a version is found for a pull request,
	// so it is read at the merge commit before the tag is looked for; with
	// a version the tag is the reference for everything.
	var content *TagContent
	var entry *reposetup.Fields
	if w.config.PR != 0 {
		merge, err := w.config.GitHub.GetPullRequestMerge(ctx, owner, repo, w.config.PR)
		if err != nil {
			return fmt.Errorf("reading pull request #%d: %w", w.config.PR, err)
		}
		if !merge.Merged {
			return notApplicableErr("pull request %s/%s#%d is %s and not merged: there is no release to wait for", owner, repo, w.config.PR, merge.State)
		}
		result.SHA = merge.MergeCommitSHA
		w.progress.Printf("pull request #%d merged as %s", w.config.PR, short(merge.MergeCommitSHA))

		content, entry, err = w.readModels(ctx, merge.MergeCommitSHA, result)
		if err != nil {
			return err
		}
		if result.ReleaseModel != ReleaseModelAutoRelease {
			return notApplicableErr("%s/%s releases through the %s workflow, which does not tag the merge commit of #%d: pass the version instead", owner, repo, result.ReleaseModel, w.config.PR)
		}
		tag, err := w.awaitTagForCommit(ctx, merge.MergeCommitSHA)
		if err != nil {
			return err
		}
		result.Tag = tag
	} else {
		version, _ := ParseVersion(w.config.Version)
		tag, sha, err := w.awaitTag(ctx, version)
		if err != nil {
			return err
		}
		result.Tag, result.SHA = tag, sha
		content, entry, err = w.readModels(ctx, sha, result)
		if err != nil {
			return err
		}
	}
	w.progress.Printf("tag %s at %s: release model %s, CI %s", result.Tag, short(result.SHA), result.ReleaseModel, result.CIModel)

	private, err := w.config.GitHub.IsPrivateRepository(ctx, owner, repo)
	if err != nil {
		return fmt.Errorf("reading the repository: %w", err)
	}
	version := strings.TrimPrefix(result.Tag, "v")

	if content.HasCircleCI() {
		w.circleci, err = w.config.CircleCI(ctx)
		if err != nil {
			return err
		}
	}

	plan := &plan{version: version, private: private, content: content, entry: entry}
	switch result.CIModel {
	case CIModelGenerated:
		if entry == nil {
			return usageErr("no team-file entry declares %s/%s, and the artifacts of its generated pipeline (%s at %s) are named by the entry", owner, repo, circleCIWorkflows, short(result.SHA))
		}
		artifacts, err := GeneratedArtifacts(*entry, repo, version, *content, private, w.config.Endpoints)
		if err != nil {
			return err
		}
		plan.setArtifacts(result, artifacts)
	case CIModelNone:
		// Nothing to derive: no CircleCI means no image and no chart.
		plan.setArtifacts(result, nil)
	}
	for _, a := range result.Artifacts {
		w.progress.Printf("expecting %s %s", a.Kind, a.Reference)
	}
	return w.loop(ctx, result, plan)
}

// plan is what the loop knows beyond the document.
type plan struct {
	version string
	private bool
	content *TagContent
	entry   *reposetup.Fields
	// derived says the expected artifacts are known; hand-written CI
	// derives them from the tag pipeline's jobs once those exist.
	derived bool
	// releaseAssets: no image and no chart; the wait is on the published
	// release and the tag's workflows.
	releaseAssets bool
	// charts are the catalog names of the chart artifacts, for --catalog.
	charts []chartArtifact
	// failures counts consecutive registry failures that were not answers.
	failures map[string]int
}

// setArtifacts records the expected artifacts; none means the wait is on
// the published release and the tag's workflows.
func (p *plan) setArtifacts(result *Result, artifacts []Artifact) {
	if artifacts == nil {
		artifacts = []Artifact{}
	}
	result.Artifacts = artifacts
	p.derived = true
	p.releaseAssets = len(artifacts) == 0
	p.charts = nil
	for _, a := range artifacts {
		if a.Kind == KindChart {
			p.charts = append(p.charts, chartArtifact{name: a.chart, catalog: a.catalog})
		}
	}
}

type chartArtifact struct{ name, catalog string }

// loop polls until every artifact is available and the tag's CI is green,
// the tag's CI failed or the deadline passed.
func (w *Waiter) loop(ctx context.Context, result *Result, p *plan) error {
	owner, repo := w.config.Owner, w.config.Repo
	for {
		state, err := w.pipelineState(ctx, result)
		if err != nil {
			return err
		}
		if state.failed {
			return ciFailedErr("the tag pipeline of %s failed: %s", result.Tag, strings.Join(state.failedJobs, ", "))
		}

		if !p.derived && result.CIModel == CIModelHandWritten && state.jobs != nil {
			artifacts, err := HandWrittenArtifacts(ctx, w.config.GitHub, owner, repo, result.SHA, p.version, *p.content, state.jobs, p.private, w.config.Endpoints)
			if err != nil {
				return err
			}
			p.setArtifacts(result, artifacts)
			for _, a := range artifacts {
				w.progress.Printf("expecting %s %s", a.Kind, a.Reference)
			}
		}

		if p.derived {
			if err := w.probe(ctx, result, p); err != nil {
				return err
			}
		}

		done := false
		switch {
		case !p.derived:
		case p.releaseAssets:
			done, err = w.releasePublished(ctx, result, state)
			if err != nil {
				return err
			}
		default:
			// The artifacts the sources name are not the whole release: a
			// repository's own tag jobs (custom.yml) push more, and a job
			// signs what it pushed after the digest resolves. The release is
			// out when the tag pipeline is green as well.
			done = allAvailable(result.Artifacts)
			if done && !state.green {
				w.progress.Printf("every artifact is available; %s", stillRunning(result.Pipeline))
				done = false
			}
			if done && w.config.Catalog {
				done, err = w.catalogLists(ctx, p)
				if err != nil {
					return err
				}
			}
		}
		if done {
			return nil
		}

		if err := w.clock.Sleep(ctx, w.interval()); err != nil {
			return w.timeout(result)
		}
	}
}

// probe asks the registry for every artifact still missing.
func (w *Waiter) probe(ctx context.Context, result *Result, p *plan) error {
	if p.failures == nil {
		p.failures = map[string]int{}
	}
	for i := range result.Artifacts {
		a := &result.Artifacts[i]
		if a.State == StateAvailable || a.Kind == KindReleaseAsset {
			continue
		}
		digest, found, err := w.config.Registry.Probe(ctx, *a)
		if err != nil {
			if ctx.Err() != nil {
				return err
			}
			var answer *RegistryAnswerError
			if errors.As(err, &answer) {
				return usageErr("%s: %v", a.Reference, err)
			}
			p.failures[a.Reference]++
			if p.failures[a.Reference] >= registryFailureLimit {
				return usageErr("probing %s failed %d times in a row: %v", a.Reference, p.failures[a.Reference], err)
			}
			w.progress.Printf("%s: probe failed (%v), retrying", a.Reference, err)
			continue
		}
		p.failures[a.Reference] = 0
		if found {
			a.Digest, a.State = digest, StateAvailable
			w.progress.Printf("%s %s available (%s)", a.Kind, a.Reference, digest)
		} else {
			w.progress.Printf("%s %s not in the registry yet", a.Kind, a.Reference)
		}
	}
	return nil
}

// registryFailureLimit is how many consecutive non-answers (a connection
// refused, a timeout) a probe tolerates before the wait ends as a tooling
// failure.
const registryFailureLimit = 3

// releasePublished is the release-assets verdict: the release exists and is
// not a draft, and every workflow of the tag finished green.
func (w *Waiter) releasePublished(ctx context.Context, result *Result, state *pipelineState) (bool, error) {
	if !state.green {
		w.progress.Printf("the tag's workflows have not all finished")
		return false, nil
	}
	release, err := w.config.GitHub.GetReleaseByTag(ctx, w.config.Owner, w.config.Repo, result.Tag)
	if githubclient.IsNotFound(err) {
		w.progress.Printf("no release for %s yet", result.Tag)
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading the release of %s: %w", result.Tag, err)
	}
	if !release.Published {
		w.progress.Printf("the release of %s is a draft", result.Tag)
		return false, nil
	}
	result.Artifacts = make([]Artifact, 0, len(release.Assets))
	for _, asset := range release.Assets {
		result.Artifacts = append(result.Artifacts, Artifact{Kind: KindReleaseAsset, Reference: asset.URL, Digest: asset.Digest, State: StateAvailable})
	}
	w.progress.Printf("release %s published with %d asset(s)", result.Tag, len(release.Assets))
	return true, nil
}

func (w *Waiter) catalogLists(ctx context.Context, p *plan) (bool, error) {
	for _, c := range p.charts {
		listed, err := w.config.CatalogIndex.Lists(ctx, c.catalog, c.name, p.version)
		if err != nil {
			return false, usageErr("reading the %s index: %v", c.catalog, err)
		}
		if !listed {
			w.progress.Printf("catalog %s does not list %s %s yet", c.catalog, c.name, p.version)
			return false, nil
		}
	}
	return true, nil
}

// awaitTag polls until one spelling of the version exists as a tag.
func (w *Waiter) awaitTag(ctx context.Context, version Version) (tag, sha string, err error) {
	for {
		for _, candidate := range version.Tags() {
			sha, err := w.config.GitHub.GetTagSHA(ctx, w.config.Owner, w.config.Repo, candidate)
			if githubclient.IsNotFound(err) {
				continue
			}
			if err != nil {
				return "", "", fmt.Errorf("reading tag %s: %w", candidate, err)
			}
			return candidate, sha, nil
		}
		w.progress.Printf("tag %s does not exist yet", version.Tags()[0])
		if err := w.clock.Sleep(ctx, w.interval()); err != nil {
			return "", "", timeoutErr("tag %s did not appear in %s/%s within %s", version.Tags()[0], w.config.Owner, w.config.Repo, w.config.Timeout)
		}
	}
}

// awaitTagForCommit polls until a tag points at the merge commit.
func (w *Waiter) awaitTagForCommit(ctx context.Context, sha string) (string, error) {
	for {
		tag, err := w.config.GitHub.FindTagForCommit(ctx, w.config.Owner, w.config.Repo, sha)
		if err != nil {
			return "", fmt.Errorf("listing tags: %w", err)
		}
		if tag != "" {
			return tag, nil
		}
		w.progress.Printf("no tag on %s yet", short(sha))
		if err := w.clock.Sleep(ctx, w.interval()); err != nil {
			return "", timeoutErr("no tag on the merge commit %s of %s/%s#%d within %s: auto-release tags within minutes when the commits warrant a bump, and never when they do not", short(sha), w.config.Owner, w.config.Repo, w.config.PR, w.config.Timeout)
		}
	}
}

// readModels reads the tag's content at sha and settles the release and CI
// models from the team-file entry and the files.
func (w *Waiter) readModels(ctx context.Context, sha string, result *Result) (*TagContent, *reposetup.Fields, error) {
	content, err := ReadTagContent(ctx, w.config.GitHub, w.config.Owner, w.config.Repo, sha)
	if err != nil {
		return nil, nil, err
	}
	entry, found, err := w.config.Entries.FindEntry(ctx, w.config.Owner, w.config.Repo)
	if err != nil {
		return nil, nil, fmt.Errorf("reading the team-file entry of %s/%s: %w", w.config.Owner, w.config.Repo, err)
	}
	if !found {
		entry = nil
		w.progress.Printf("no team-file entry declares %s/%s: the models come from the tag's files", w.config.Owner, w.config.Repo)
	}
	models, err := ResolveModels(entry, *content)
	if err != nil {
		return nil, nil, err
	}
	if models.Warning != "" {
		w.progress.Printf("%s", models.Warning)
		if w.config.Warn != nil {
			w.config.Warn(models.Warning)
		}
	}
	result.ReleaseModel, result.CIModel = models.Release, models.CI
	return content, entry, nil
}

// interval is the pause between polls: between the floor and the ceiling,
// following the rate limit GitHub reported last.
func (w *Waiter) interval() time.Duration {
	if w.config.Rate == nil {
		return IntervalFloor
	}
	rate := w.config.Rate()
	if !rate.Known {
		return IntervalFloor
	}
	if rate.Remaining <= 0 {
		return IntervalCeiling
	}
	untilReset := rate.Reset.Sub(w.clock.Now())
	if untilReset <= 0 {
		return IntervalFloor
	}
	d := untilReset / time.Duration(rate.Remaining)
	if d < IntervalFloor {
		return IntervalFloor
	}
	if d > IntervalCeiling {
		return IntervalCeiling
	}
	return d
}

func (w *Waiter) timeout(result *Result) error {
	var missing []string
	for _, a := range result.Artifacts {
		if a.State != StateAvailable {
			missing = append(missing, a.Kind+" "+a.Reference)
		}
	}
	// What the tag pipeline was still doing, so the reason says where the
	// build stood: a workflow running, or one whose jobs never appeared.
	unfinished := ""
	if p := result.Pipeline; p != nil && len(p.Unfinished) > 0 {
		unfinished = "; " + stillRunning(p)
	}
	switch {
	case result.Tag == "":
		return timeoutErr("no tag within %s", w.config.Timeout)
	case len(missing) > 0:
		return timeoutErr("not available within %s: %s%s", w.config.Timeout, strings.Join(missing, ", "), unfinished)
	case result.Pipeline == nil && result.CIModel != CIModelNone:
		return timeoutErr("no CircleCI pipeline for %s within %s: the tag's webhook may not have reached CircleCI", result.Tag, w.config.Timeout)
	case len(result.Artifacts) > 0 && unfinished != "":
		return timeoutErr("every artifact of %s is available, the tag pipeline did not finish within %s%s", result.Tag, w.config.Timeout, unfinished)
	}
	return timeoutErr("%s was not released within %s%s", result.Tag, w.config.Timeout, unfinished)
}

// stillRunning names what keeps the tag pipeline from being green, for the
// progress line of a wait that outlasts its artifacts and the timeout's
// reason.
func stillRunning(p *Pipeline) string {
	switch {
	case p == nil:
		return "no CircleCI pipeline for the tag yet"
	case len(p.Unfinished) == 0:
		return fmt.Sprintf("pipeline %d has no successful workflow", p.Number)
	}
	return fmt.Sprintf("pipeline %d unfinished: %s", p.Number, strings.Join(p.Unfinished, ", "))
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
