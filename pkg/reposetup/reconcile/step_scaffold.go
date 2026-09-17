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
	return s.plan(sr, "render the scaffold and push it as the first commit on "+s.branch(), func() error {
		if err := r.pushScaffold(ctx, s, sr, empty); err != nil {
			return err
		}
		s.empty = false
		return r.chartFindings(ctx, s, sr)
	})
}

// scaffoldState says whether the default branch has no commits, or only the
// initial commit of the creation (a lone README).
func (r *Runner) scaffoldState(ctx context.Context, s *run) (empty, initialOnly bool, err error) {
	_, resp, err := r.GitHub.Repositories.ListCommits(ctx, s.owner, s.name, &github.CommitsListOptions{
		SHA:         s.branch(),
		ListOptions: github.ListOptions{PerPage: 1},
	})
	switch {
	case resp != nil && resp.Response != nil && resp.StatusCode == 409:
		return true, false, nil // "Git Repository is empty"
	case err != nil:
		return false, false, err
	}
	_, dir, resp, err := r.GitHub.Repositories.GetContents(ctx, s.owner, s.name, "", &github.RepositoryContentGetOptions{Ref: s.branch()})
	switch {
	case isNotFound(resp, err):
		return true, false, nil
	case err != nil:
		return false, false, err
	}
	return false, len(dir) == 1 && dir[0].GetName() == readmeFile, nil
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
		Message: new(fmt.Sprintf("feat: initial scaffold of %s from %s\n\nRendered by devctl for the entry in repositories/%s.yaml.", s.name, scaffoldOrigin(scaffold.Template), s.req.Team)),
		Tree:    &github.Tree{SHA: tree.SHA},
	}, nil)
	if err != nil {
		return err
	}
	_, _, err = r.GitHub.Git.UpdateRef(ctx, s.owner, s.name, "heads/"+s.branch(), github.UpdateRef{
		SHA:   commit.GetSHA(),
		Force: new(true),
	})
	return err
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
// first app-build-suite run would fail on, and the default icon.
func (r *Runner) chartFindings(ctx context.Context, s *run, sr *StepResult) error {
	if s.fields.Gen == nil || !reposetup.HasChart(s.fields.Gen.Flavours) {
		return nil
	}
	chartDir := "helm/" + s.name
	data, found, err := r.fileContent(ctx, s.owner, s.name, chartDir+"/Chart.yaml", s.branch())
	if err != nil {
		return err
	}
	if !found {
		s.report(sr, FindingABSPrerequisite,
			fmt.Sprintf("%s has no chart at %s/Chart.yaml", s.slug(), chartDir),
			fmt.Sprintf("add the chart under %s (the app flavour builds it) or drop the app flavour from the entry", chartDir))
		return nil
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

func scaffoldOrigin(t reposetup.Template) string {
	if repo := t.Repository(); repo != "" {
		return repo
	}
	return "the minimal scaffold"
}
