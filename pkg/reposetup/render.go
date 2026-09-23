package reposetup

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/giantswarm/microerror"
)

// Renderer renders the scaffold of an accepted declaration: the template
// checkout with its placeholders replaced, the chart of the chart template
// when the flavours produce one the template lacks, the files devctl
// generates for the declared flavours and language, the chart tests the
// generated pipeline runs where the template carries none, CODEOWNERS for
// the team and the chart's team annotation. The generated files are the ones
// align-files writes for the same declaration with the same devctl, so the
// first align run after the scaffold is pushed changes nothing.
type Renderer struct {
	// Templates fetches the template repositories; nil fetches the tarball
	// of each template's main branch from GitHub ([GitHubTemplates]).
	Templates TemplateSource
	// Log receives the generators' output; nil discards it.
	Log io.Writer
}

// RenderRequest is one scaffold to render.
type RenderRequest struct {
	// Team is the slug of the team file the entry lives in (team-bumblebee):
	// CODEOWNERS names it and the chart's team annotation carries its short
	// name.
	Team string
	// Entry is an accepted entry of [Validator.Validate]; its Rendered
	// declaration, Template and Chart are read.
	Entry Entry
	// Dir is the directory the scaffold is rendered into. It is created;
	// an existing directory has to be empty.
	Dir string
	// Options selects among [Entry.Options] by name; an option left out
	// takes its default.
	Options map[string]string
}

// Scaffold describes a rendered scaffold.
type Scaffold struct {
	// Dir the scaffold was rendered into.
	Dir string `json:"dir"`
	// Template it was rendered from.
	Template Template `json:"template"`
	// Chart is the template whose chart was added at helm/<name>; empty
	// when the template carries its own chart or the flavours produce none.
	Chart Template `json:"chart,omitempty"`
	// Options in effect, defaults included.
	Options map[string]string `json:"options,omitempty"`
	// Commands are the `devctl gen …` command lines that produced the
	// generated files, as align-files runs them.
	Commands []string `json:"commands"`
	// Files are the scaffold's paths relative to Dir, sorted.
	Files []string `json:"files"`
}

// Render renders the scaffold of req.Entry into req.Dir.
//
// The generators read the repository from the process working directory,
// as `devctl gen` does; Render changes it to req.Dir while they run, under
// a lock, and restores it. Concurrent renders serialize on that lock.
func (r Renderer) Render(ctx context.Context, req RenderRequest) (*Scaffold, error) {
	if !req.Entry.Accepted {
		return nil, microerror.Maskf(invalidConfigError, "entry %q is not accepted: %d problems", req.Entry.Name, len(req.Entry.Problems))
	}
	if req.Entry.Template == "" {
		return nil, microerror.Maskf(invalidConfigError, "entry %q derives no template", req.Entry.Name)
	}
	if req.Team == "" {
		return nil, microerror.Maskf(invalidConfigError, "%T.Team must not be empty", req)
	}
	if req.Dir == "" {
		return nil, microerror.Maskf(invalidConfigError, "%T.Dir must not be empty", req)
	}

	// The entry as the dry run rendered it, defaults written out.
	tf, err := ParseTeamFile(req.Team, strings.NewReader(req.Entry.Rendered))
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

	template := req.Entry.Template
	options, err := resolveOptions(template, req.Options)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	dir, err := filepath.Abs(req.Dir)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	if err := prepareDir(dir); err != nil {
		return nil, microerror.Mask(err)
	}

	templates := r.Templates
	if templates == nil {
		templates = GitHubTemplates{}
	}
	if repository := template.Repository(); repository != "" {
		if err := templates.Fetch(ctx, repository, dir); err != nil {
			return nil, microerror.Mask(err)
		}
	}

	subst := substitutions{
		Name:         fields.Name,
		Team:         req.Team,
		Description:  fields.Description,
		UpstreamRepo: options[OptionUpstreamRepo],
	}
	if subst.UpstreamRepo == "" {
		subst.UpstreamRepo = fmt.Sprintf("https://github.com/%s/%s", DefaultOwner, fields.Name)
	}
	if err := replacePlaceholders(dir, template, subst); err != nil {
		return nil, microerror.Mask(err)
	}
	if err := writeCommonFiles(dir, template, subst); err != nil {
		return nil, microerror.Mask(err)
	}
	if req.Entry.Chart != "" {
		if err := addChart(ctx, templates, req.Entry.Chart, dir, subst); err != nil {
			return nil, microerror.Mask(err)
		}
	}
	if err := writeChartOptions(dir, fields.Name, options); err != nil {
		return nil, microerror.Mask(err)
	}

	knows, err := knownGenFlags()
	if err != nil {
		return nil, microerror.Mask(err)
	}
	_, helmErr := os.Stat(filepath.Join(dir, "helm"))
	commands := genCommands(fields, genContext{HasHelm: helmErr == nil, Knows: knows})
	if err := runGen(ctx, dir, r.Log, commands); err != nil {
		return nil, microerror.Mask(err)
	}
	// After the generators: the chart tests follow the ATS dependency file
	// `devctl gen circleci` emits, and are the repository's own from then on.
	if err := writeChartTests(dir, fields.Name); err != nil {
		return nil, microerror.Mask(err)
	}

	files, err := listFiles(dir)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	s := &Scaffold{
		Dir:      dir,
		Template: template,
		Chart:    req.Entry.Chart,
		Options:  options,
		Files:    files,
	}
	for _, argv := range commands {
		s.Commands = append(s.Commands, commandLine(argv))
	}

	return s, nil
}

// chartDirs are the directories of the chart template that make up its
// chart: the chart itself and app-build-suite's configuration pointing at
// it. Everything else in the chart template is the derived template's or
// generated.
var chartDirs = []string{"helm", ".abs"}

// addChart renders the chart template the way the chart-only scaffold is
// rendered, in a temporary directory, and copies chartDirs into the
// scaffold at dir.
func addChart(ctx context.Context, templates TemplateSource, chart Template, dir string, s substitutions) error {
	tmp, err := os.MkdirTemp("", "reposetup-chart-")
	if err != nil {
		return microerror.Mask(err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	if err := templates.Fetch(ctx, chart.Repository(), tmp); err != nil {
		return microerror.Mask(err)
	}
	if err := replacePlaceholders(tmp, chart, s); err != nil {
		return microerror.Mask(err)
	}
	for _, d := range chartDirs {
		if err := copyTree(filepath.Join(tmp, d), filepath.Join(dir, d)); err != nil {
			return microerror.Mask(err)
		}
	}
	return nil
}

// prepareDir creates dir or checks that it is empty.
func prepareDir(dir string) error {
	entries, err := os.ReadDir(dir)
	switch {
	case os.IsNotExist(err):
		return os.MkdirAll(dir, dirMode)
	case err != nil:
		return microerror.Mask(err)
	case len(entries) > 0:
		return microerror.Maskf(invalidConfigError, "directory %s is not empty", dir)
	}
	return nil
}

// listFiles returns the regular files under dir, relative, slash-separated,
// sorted.
func listFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return microerror.Mask(err)
		}
		if d.IsDir() {
			if d.Name() == gitDir {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return microerror.Mask(err)
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, microerror.Mask(err)
	}
	sort.Strings(files)
	return files, nil
}
