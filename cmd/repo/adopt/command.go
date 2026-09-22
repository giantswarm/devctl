// Package adopt is `devctl repo adopt`: an existing, undeclared repository
// declared in a team's file.
package adopt

import (
	"fmt"
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/cmd/repo/internal/write"
	"github.com/giantswarm/devctl/v8/pkg/gen"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

const (
	name      = "adopt"
	shortDesc = "Declare a repository that exists on GitHub and no team file declares"
	longDesc  = `Adopt a repository of the org that exists on GitHub and no team file declares
(the inventory's Unassigned scope): the entry is added to the team's file in
a pull request opened as you with auto-merge armed, and the ask with the
Approve button goes to the team's channel -- an existing name is a plain
addition, so a member other than you approves. The entry is validated against
the repositories schema, not the creation rules. Without --align the
reconciler's run of the merge checks the repository and reports the drift;
with it the run aligns the repository with its declared set-up and the
company baseline. --lifecycle deprecated or archived adopts the repository
and ends its life in the one pull request. A name that is free on GitHub is
refused (create it with ` + "`devctl repo create`" + `); one declared already is
refused too (edit, transfer or set its lifecycle instead).

Examples:
  devctl repo adopt old-tool --team team-bumblebee --component-type tool --language go --dry-run
  devctl repo adopt old-tool --team team-bumblebee --component-type tool --language go --reason "ours since 2024"
  devctl repo adopt dead-repo --team team-planeteers --lifecycle archived --reason "no commit since 2023"`
)

type Config struct {
	Logger *logrus.Logger
	Stderr io.Writer
	Stdout io.Writer
}

func New(config Config) (*cobra.Command, error) {
	if config.Logger == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.Logger must not be empty", config)
	}
	if config.Stderr == nil {
		config.Stderr = os.Stderr
	}
	if config.Stdout == nil {
		config.Stdout = os.Stdout
	}

	f := &flag{}
	r := &runner{flag: f, logger: config.Logger, stderr: config.Stderr, stdout: config.Stdout, open: client.Open}

	c := &cobra.Command{
		Use:   fmt.Sprintf("%s [flags] [OWNER/]REPOSITORY", name),
		Short: shortDesc,
		Long:  longDesc,
		Args:  cobra.ExactArgs(1),
		RunE:  r.Run,
	}
	f.Init(c)
	return c, nil
}

type flag struct {
	write.Flags

	Team          string
	ComponentType string
	Description   string
	Visibility    string
	Language      string
	Flavours      []string
	CIGenerate    string
	Align         bool
	Lifecycle     string
}

func (f *flag) Init(cmd *cobra.Command) {
	f.Flags.Init(cmd, "write nothing")
	cmd.Flags().StringVar(&f.Team, "team", "", "The adopting team's file, as its GitHub team slug (team-bumblebee). Required.")
	cmd.Flags().StringVar(&f.ComponentType, "component-type", "", fmt.Sprintf("componentType of the entry: %s.", oneOf(reposetup.EmbeddedFieldValues("componentType"))))
	cmd.Flags().StringVar(&f.Description, "description", "", "description of the entry; the reconciler sets it on the repository.")
	cmd.Flags().StringVar(&f.Visibility, "visibility", "", fmt.Sprintf("visibility of the entry: %s.", oneOf(reposetup.EmbeddedFieldValues("visibility"))))
	cmd.Flags().StringVar(&f.Language, "language", "", fmt.Sprintf("gen.language: %s.", oneOf(gen.AllLanguages())))
	cmd.Flags().StringArrayVar(&f.Flavours, "flavour", nil, fmt.Sprintf("gen.flavours entry, repeatable: %s.", oneOf(gen.AllFlavours())))
	cmd.Flags().StringVar(&f.CIGenerate, "ci-generate", "", "gen.ci.generate: true to generate the CircleCI configuration, false to keep the repository's own; unset leaves the field out.")
	cmd.Flags().BoolVar(&f.Align, "align", false, "Opt the repository in to alignment (align: true): the reconciler changes it to its declared set-up on every trigger.")
	cmd.Flags().StringVar(&f.Lifecycle, "lifecycle", "", "deprecated or archived: adopt the repository and end its life in the one pull request.")
}

func (f *flag) Validate() error {
	if err := f.Flags.Validate(); err != nil {
		return microerror.Mask(err)
	}
	if f.Team == "" {
		return microerror.Maskf(client.InvalidFlagError, "--team must name the adopting team")
	}
	switch f.CIGenerate {
	case "", "true", "false":
	default:
		return microerror.Maskf(client.InvalidFlagError, "--ci-generate must be true or false, got %q", f.CIGenerate)
	}
	switch f.Lifecycle {
	case "", "deprecated", "archived":
	default:
		return microerror.Maskf(client.InvalidFlagError, "--lifecycle must be deprecated or archived, got %q (deleted: declare first, then `devctl repo set-lifecycle`)", f.Lifecycle)
	}
	return nil
}

// entry is the declaration as it goes into the team file, from the flags
// that are set.
func (f *flag) entry() map[string]any {
	entry := map[string]any{}
	set := func(key, value string) {
		if value != "" {
			entry[key] = value
		}
	}
	set("componentType", f.ComponentType)
	set("description", f.Description)
	set("visibility", f.Visibility)
	set("lifecycle", f.Lifecycle)
	if f.Align {
		entry["align"] = true
	}
	g := map[string]any{}
	if len(f.Flavours) > 0 {
		g["flavours"] = f.Flavours
	}
	if f.Language != "" {
		g["language"] = f.Language
	}
	if f.CIGenerate != "" {
		g["ci"] = map[string]any{"generate": f.CIGenerate == "true"}
	}
	if len(g) > 0 {
		entry["gen"] = g
	}
	return entry
}

func oneOf(values []string) string {
	out := ""
	for i, v := range values {
		switch {
		case i == 0:
			out = v
		case i == len(values)-1:
			out += " or " + v
		default:
			out += ", " + v
		}
	}
	return out
}

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}
