// Package renovateplan is `devctl repo renovate-plan`: what align-files does
// about a repository's Renovate configuration, decided by the function the
// scaffold of `devctl repo create` uses.
package renovateplan

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/renovate"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

const (
	name      = "renovate-plan"
	shortDesc = "Print what align-files does about a repository's Renovate configuration"
	longDesc  = `Print, as JSON, what align-files does about the Renovate configuration of a
repository declared in a giantswarm/github team file: {"generate": true} runs
devctl gen renovate, {"patchReviewers": true} runs devctl gen renovate
reviewers on the configuration the repository keeps, both false does
neither.

The decision is the one the scaffold of devctl repo create takes, so a
created repository and its align runs agree: nothing for an entry that
declares renovate: false; renovate.json5 generated for an entry with
gen.flavours that is no fork line on generated CI, for the customer flavour,
and when the checkout at --dir has no Renovate configuration; the chore
reviewers patched into the configuration every other repository keeps.

Examples:
  devctl repo renovate-plan --team-file repositories/team-rocket.yaml --name my-app --dir my-app`
)

type Config struct {
	Stdout io.Writer
}

func New(config Config) (*cobra.Command, error) {
	if config.Stdout == nil {
		config.Stdout = os.Stdout
	}

	var teamFile, repo, dir string
	c := &cobra.Command{
		Use:   name,
		Short: shortDesc,
		Long:  longDesc,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return microerror.Mask(run(config.Stdout, teamFile, repo, dir))
		},
	}
	c.Flags().StringVar(&teamFile, "team-file", "", "The giantswarm/github team file that declares the repository.")
	c.Flags().StringVar(&repo, "name", "", "The repository's name, as its entry declares it.")
	c.Flags().StringVar(&dir, "dir", ".", "The repository's checkout, read for an existing Renovate configuration.")
	_ = c.MarkFlagRequired("team-file")
	_ = c.MarkFlagRequired("name")

	return c, nil
}

func run(out io.Writer, teamFile, repo, dir string) error {
	tf, err := reposetup.ReadTeamFile(teamFile)
	if err != nil {
		return microerror.Mask(err)
	}
	d, ok := tf.Entry(repo)
	if !ok {
		return microerror.Maskf(entryNotFoundError, "%s declares no repository %q", teamFile, repo)
	}
	fields, err := d.Fields()
	if err != nil {
		return microerror.Mask(err)
	}
	hasConfig, err := renovate.HasConfig(dir)
	if err != nil {
		return microerror.Mask(err)
	}

	doc, err := json.Marshal(reposetup.PlanRenovate(fields, hasConfig))
	if err != nil {
		return microerror.Mask(err)
	}
	_, err = fmt.Fprintln(out, string(doc))
	return microerror.Mask(err)
}

var entryNotFoundError = &microerror.Error{
	Kind: "entryNotFoundError",
}
