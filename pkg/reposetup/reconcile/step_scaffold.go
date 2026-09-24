package reconcile

import (
	"context"
	"encoding/base64"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/google/go-github/v92/github"
	"gopkg.in/yaml.v3"

	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

const (
	// defaultChartIcon is the icon the scaffold gives a chart; the inventory
	// flags it until the team replaces it.
	defaultChartIcon = "https://s.giantswarm.io/app-icons/giantswarm/1/light.svg"
	// chartTeamAnnotation is the Chart.yaml annotation app-build-suite's
	// validator C0001 requires.
	chartTeamAnnotation = "io.giantswarm.application.team"
	// genCircleCIRefusal is the message `devctl gen circleci` refuses with
	// when the declaration yields no job.
	genCircleCIRefusal = "no jobs would be generated"
	readmeFile         = "README.md"
	// helmDir is the directory the charts of a repository live under.
	helmDir = "helm"
)

// stepScaffold pushes the rendered scaffold as the first commit on the
// default branch of an empty repository (or one holding only the initial
// README of the creation), before protection. On a repository that has its
// scaffold the step checks the chart's build prerequisites and reports what
// app-build-suite would fail on and the default icon.
func (r *Runner) stepScaffold(ctx context.Context, s *run, sr *StepResult) error {
	empty, initialOnly, err := r.scaffoldState(ctx, s)
	if err != nil {
		return err
	}
	s.empty = empty
	if !empty && !initialOnly {
		sr.Summary = "present"
		return r.chartFindings(ctx, s, sr)
	}
	if r.Renderer == nil && s.req.Mode == ModeRepair {
		return fmt.Errorf("the repository has no scaffold and this runner has no renderer")
	}
	return s.plan(sr, s.scaffoldChange(s.branch()), func() error {
		if err := r.pushScaffold(ctx, s, sr, empty); err != nil {
			return err
		}
		s.empty = false
		return r.chartFindings(ctx, s, sr)
	})
}

// scaffoldState says whether the default branch has no commits, or only the
// initial commit of the creation (a lone README): one listing of the root,
// which a branch without commits answers 404 ("This repository is empty").
func (r *Runner) scaffoldState(ctx context.Context, s *run) (empty, initialOnly bool, err error) {
	_, dir, resp, err := r.GitHub.Repositories.GetContents(ctx, s.owner, s.name, "", &github.RepositoryContentGetOptions{Ref: s.branch()})
	switch {
	case isNotFound(resp, err):
		return true, false, nil
	case err != nil:
		return false, false, err
	}
	return false, len(dir) == 1 && dir[0].GetName() == readmeFile, nil
}

// headCommit is the SHA at the head of the default branch, "" on a branch
// without commits.
func (r *Runner) headCommit(ctx context.Context, s *run) (string, error) {
	commits, resp, err := r.GitHub.Repositories.ListCommits(ctx, s.owner, s.name, &github.CommitsListOptions{
		SHA:         s.branch(),
		ListOptions: github.ListOptions{PerPage: 1},
	})
	switch {
	case resp != nil && resp.Response != nil && resp.StatusCode == 409:
		return "", nil // "Git Repository is empty"
	case err != nil:
		return "", err
	case len(commits) == 0:
		return "", nil
	}
	return commits[0].GetSHA(), nil
}

// pushScaffold renders the scaffold and makes it the only commit on the
// default branch: the Git Data API cannot write to an empty repository, so
// the README goes first through the contents API when needed; the tree is
// then committed without a parent and the branch moved to it.
func (r *Runner) pushScaffold(ctx context.Context, s *run, sr *StepResult, empty bool) error {
	dir, err := os.MkdirTemp("", "reposetup-"+s.name+"-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	scaffold, err := r.Renderer.Render(ctx, reposetup.RenderRequest{
		Team:    s.req.Team,
		Entry:   s.req.Entry,
		Dir:     dir,
		Options: s.req.RenderOptions,
	})
	if err != nil {
		if strings.Contains(err.Error(), genCircleCIRefusal) {
			s.report(sr, FindingGenCircleCIRefused,
				fmt.Sprintf("the CircleCI generator refuses the declaration of %s: %s", s.slug(), afterRefusal(err.Error())),
				fmt.Sprintf("in repositories/%s.yaml set gen.language to go or node, add a root Dockerfile to the repository, or add the app flavour; then rerun", s.req.Team))
			return errReported
		}
		return err
	}

	if empty {
		readme := []byte("# " + s.name + "\n")
		if data, err := os.ReadFile(filepath.Join(dir, readmeFile)); err == nil { //nolint:gosec // dir is the temp directory the scaffold was rendered into
			readme = data
		}
		_, _, err := r.GitHub.Repositories.CreateFile(ctx, s.owner, s.name, readmeFile, &github.RepositoryContentFileOptions{
			Message: new("Initialize repository"),
			Content: readme,
			Branch:  new(s.branch()),
		})
		if err != nil {
			return err
		}
	}

	entries, err := r.treeEntries(ctx, s, dir, scaffold.Files)
	if err != nil {
		return err
	}
	tree, _, err := r.GitHub.Git.CreateTree(ctx, s.owner, s.name, "", entries)
	if err != nil {
		return err
	}
	// The conventional subject is load-bearing: the scaffold's auto-release
	// workflow reads the version to tag from the conventional commits since
	// the last tag and drops every other commit (cliff.toml's
	// filter_unconventional), so a first commit without the prefix leaves the
	// repository without its v0.1.0 for good.
	commit, _, err := r.GitHub.Git.CreateCommit(ctx, s.owner, s.name, github.Commit{
		Message: new(fmt.Sprintf("%s%s\n\nRendered by devctl for the entry in repositories/%s.yaml%s.", scaffoldSubjectPrefix(s.name), scaffoldOrigin(scaffold.Template), s.req.Team, chartClause(scaffold.Chart, s.name))),
		Tree:    &github.Tree{SHA: tree.SHA},
	}, nil)
	if err != nil {
		return err
	}
	_, _, err = r.GitHub.Git.UpdateRef(ctx, s.owner, s.name, "heads/"+s.branch(), github.UpdateRef{
		SHA:   commit.GetSHA(),
		Force: new(true),
	})
	if err != nil {
		return err
	}
	s.scaffoldSHA = commit.GetSHA()
	return nil
}

// treeEntries turns the rendered files into tree entries: text inline,
// binary content as blobs, executables with their mode.
func (r *Runner) treeEntries(ctx context.Context, s *run, dir string, files []string) ([]*github.TreeEntry, error) {
	entries := make([]*github.TreeEntry, 0, len(files))
	for _, rel := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(p) //nolint:gosec // p is a rendered file inside the scaffold's temp directory
		if err != nil {
			return nil, err
		}
		mode := "100644"
		if info.Mode()&fs.ModePerm&0o111 != 0 {
			mode = "100755"
		}
		entry := &github.TreeEntry{Path: new(rel), Mode: new(mode), Type: new("blob")}
		if utf8.Valid(data) {
			entry.Content = new(string(data))
		} else {
			blob, _, err := r.GitHub.Git.CreateBlob(ctx, s.owner, s.name, github.Blob{
				Content:  new(base64.StdEncoding.EncodeToString(data)),
				Encoding: new("base64"),
			})
			if err != nil {
				return nil, err
			}
			entry.SHA = blob.SHA
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// chartFindings reads the chart of a chart repository and reports what the
// first app-build-suite run would fail on, and the default icon. The chart
// of a template is not read: it is not at helm/<name>, carries placeholders
// and is built from a rendered copy by the template's own pipeline. Any
// other chart is the one the entry declares the pipeline builds:
// helm/<gen.ci.chartName> when set, helm/<repository> otherwise.
func (r *Runner) chartFindings(ctx context.Context, s *run, sr *StepResult) error {
	if s.fields.Gen == nil || !reposetup.HasChart(s.fields.Gen.Flavours) {
		return nil
	}
	if s.isTemplate() {
		sr.Summary = "present; the chart of a template carries placeholders and is built from a rendered copy by its own pipeline"
		return nil
	}
	// The chart the pipeline builds: helm/<gen.ci.chartName> when set,
	// helm/<repository> otherwise.
	chartDir := helmDir + "/" + s.chartName()
	data, found, err := r.fileContent(ctx, s.owner, s.name, chartDir+"/Chart.yaml", s.branch())
	if err != nil {
		return err
	}
	if !found {
		return r.reportMissingChart(ctx, s, sr, chartDir)
	}
	var chart struct {
		Icon        string            `yaml:"icon"`
		Annotations map[string]string `yaml:"annotations"`
	}
	if err := yaml.Unmarshal(data, &chart); err != nil {
		s.report(sr, FindingABSPrerequisite, fmt.Sprintf("%s/Chart.yaml is not valid YAML: %v", chartDir, err), "fix the chart's Chart.yaml")
		return nil
	}
	if chart.Annotations[chartTeamAnnotation] == "" {
		s.report(sr, FindingABSPrerequisite,
			fmt.Sprintf("%s/Chart.yaml lacks the %s annotation (app-build-suite C0001 HasTeamLabel)", chartDir, chartTeamAnnotation),
			fmt.Sprintf("add annotations.%s: %s to Chart.yaml", chartTeamAnnotation, strings.TrimPrefix(s.req.Team, "team-")))
	}
	switch chart.Icon {
	case "":
		s.report(sr, FindingABSPrerequisite,
			fmt.Sprintf("%s/Chart.yaml has no icon (app-build-suite C0002 IconExists)", chartDir),
			"set icon in Chart.yaml to a square SVG or PNG on s.giantswarm.io or an allowed domain")
	case defaultChartIcon:
		s.report(sr, FindingDefaultIcon,
			fmt.Sprintf("%s/Chart.yaml carries the default Giant Swarm icon", chartDir),
			"replace icon in Chart.yaml with the application's own icon")
	}
	_, found, err = r.fileContent(ctx, s.owner, s.name, chartDir+"/values.schema.json", s.branch())
	if err != nil {
		return err
	}
	if !found {
		s.report(sr, FindingABSPrerequisite,
			fmt.Sprintf("%s has no values.schema.json (app-build-suite F0001 HasValuesSchema)", chartDir),
			fmt.Sprintf("add %s/values.schema.json describing values.yaml (helm schema-gen, or copy the template-app's)", chartDir))
	}
	return nil
}

// chartName is the name of the chart the entry declares the repository
// builds: gen.ci.chartName when set (the generated CircleCI builds
// helm/<chartName>; docs-proxy ships helm/docs-proxy-app), the repository's
// name otherwise.
func (s *run) chartName() string {
	if s.fields.Gen != nil && s.fields.Gen.CI != nil && s.fields.Gen.CI.ChartName != "" {
		return s.fields.Gen.CI.ChartName
	}
	return s.name
}

// reportMissingChart reports a chart repository without a chart at the
// declared directory. The charts the repository does have under helm/ are
// named in the fix: a chart under another name — a repository renamed on
// GitHub that kept its chart, one shipping helm/<name>-app — is declared
// with gen.ci.chartName, the remedy the generic fix text does not name.
func (r *Runner) reportMissingChart(ctx context.Context, s *run, sr *StepResult, chartDir string) error {
	charts, err := r.chartsUnderHelm(ctx, s)
	if err != nil {
		return err
	}
	var fix string
	switch {
	case len(charts) == 1:
		fix = fmt.Sprintf("the chart is %s/%s: set gen.ci.chartName: %s on the entry in repositories/%s.yaml, or rename the chart directory and its name to %s",
			helmDir, charts[0], charts[0], s.req.Team, s.chartName())
	case len(charts) > 1:
		fix = fmt.Sprintf("the charts under %s/ are %s: set gen.ci.chartName on the entry in repositories/%s.yaml to the one the pipeline builds",
			helmDir, describe(charts), s.req.Team)
	case s.chartName() != s.name:
		fix = fmt.Sprintf("add the chart under %s (gen.ci.chartName names it) or drop gen.ci.chartName and the app flavour from the entry", chartDir)
	default:
		fix = fmt.Sprintf("add the chart under %s (the app flavour builds it), set gen.ci.chartName when the chart is under another %s/ directory, or drop the app flavour from the entry", chartDir, helmDir)
	}
	s.report(sr, FindingABSPrerequisite, fmt.Sprintf("%s has no chart at %s/Chart.yaml", s.slug(), chartDir), fix)
	return nil
}

// chartsUnderHelm lists the charts the repository has under helm/: the
// directories with a Chart.yaml, in listing order; none without the
// directory.
func (r *Runner) chartsUnderHelm(ctx context.Context, s *run) ([]string, error) {
	_, dir, resp, err := r.GitHub.Repositories.GetContents(ctx, s.owner, s.name, helmDir, &github.RepositoryContentGetOptions{Ref: s.branch()})
	switch {
	case isNotFound(resp, err):
		return nil, nil
	case err != nil:
		return nil, err
	}
	var charts []string
	for _, entry := range dir {
		if entry.GetType() != "dir" {
			continue
		}
		_, found, err := r.fileContent(ctx, s.owner, s.name, entry.GetPath()+"/Chart.yaml", s.branch())
		if err != nil {
			return nil, err
		}
		if found {
			charts = append(charts, entry.GetName())
		}
	}
	return charts, nil
}

// fileContent reads one file of a repository at ref; found is false on 404.
// A file above the contents API's size limit is read as a blob.
func (r *Runner) fileContent(ctx context.Context, owner, repo, path, ref string) ([]byte, bool, error) {
	fc, _, resp, err := r.GitHub.Repositories.GetContents(ctx, owner, repo, path, &github.RepositoryContentGetOptions{Ref: ref})
	switch {
	case isNotFound(resp, err):
		return nil, false, nil
	case err != nil:
		return nil, false, err
	case fc == nil:
		return nil, false, fmt.Errorf("%s/%s: %s is a directory", owner, repo, path)
	}
	content, err := fc.GetContent()
	if err != nil {
		return nil, false, err
	}
	if content == "" && fc.GetSize() > 0 && fc.GetSHA() != "" {
		data, _, err := r.GitHub.Git.GetBlobRaw(ctx, owner, repo, fc.GetSHA())
		if err != nil {
			return nil, false, err
		}
		return data, true, nil
	}
	return []byte(content), true, nil
}

func afterRefusal(msg string) string {
	if i := strings.Index(msg, genCircleCIRefusal); i >= 0 {
		return msg[i:]
	}
	return msg
}

// scaffoldChange is the plan of the scaffold step: what it pushes, and
// onto which branch.
func (s *run) scaffoldChange(branch string) string {
	return "render the scaffold" + chartClause(s.req.Entry.Chart, s.name) + " and push it as the first commit on " + branch
}

// chartClause names the chart a scaffold carries beside its template, for
// the plan and the scaffold commit; empty when the template is all there is.
func chartClause(chart reposetup.Template, name string) string {
	if chart == "" {
		return ""
	}
	return fmt.Sprintf(" with the chart of %s at helm/%s", chart, name)
}

// scaffoldSubjectPrefix is the start of the scaffold commit's subject, up to the
// template it was rendered from: what marks a tag on that commit as the
// first release of a repository the platform created.
func scaffoldSubjectPrefix(name string) string {
	return "feat: initial scaffold of " + name + " from "
}

func scaffoldOrigin(t reposetup.Template) string {
	if repo := t.Repository(); repo != "" {
		return repo
	}
	return "the minimal scaffold"
}
