package create

import (
	"fmt"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/gen"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

const (
	outputText = "text"
	outputJSON = "json"
)

type flag struct {
	GithubTokenEnvVar string
	Team              string
	Name              string
	ComponentType     string
	Flavours          []string
	Language          string
	Description       string
	Visibility        string
	Owner             string
	DryRun            bool
	Output            string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVar(&f.GithubTokenEnvVar, "github-token-envvar", "GITHUB_TOKEN", "Environment variable holding your GitHub token; the gh CLI's login when it is unset.")
	cmd.Flags().StringVar(&f.Team, "team", "", "Team that owns the repository (bumblebee or team-bumblebee): the entry goes into its team file.")
	cmd.Flags().StringVar(&f.Name, "name", "", "Repository name, the slug in https://github.com/giantswarm/<name>.")
	cmd.Flags().StringVar(&f.ComponentType, "component-type", "", fmt.Sprintf("componentType of the entry: %s.", oneOf(reposetup.EmbeddedFieldValues("componentType"))))
	cmd.Flags().StringArrayVar(&f.Flavours, "flavour", nil, fmt.Sprintf("gen.flavours entry, repeatable: %s.", oneOf(gen.AllFlavours())))
	cmd.Flags().StringVar(&f.Language, "language", "", fmt.Sprintf("gen.language: %s.", oneOf(gen.AllLanguages())))
	cmd.Flags().StringVar(&f.Description, "description", "", "Description of the repository, set on GitHub by the reconciler.")
	cmd.Flags().StringVar(&f.Visibility, "visibility", "", fmt.Sprintf("Visibility of the repository: %s.", oneOf(reposetup.EmbeddedFieldValues("visibility"))))
	cmd.Flags().StringVar(&f.Owner, "owner", reposetup.DefaultOwner, "GitHub organisation the repository is created in and whose teams the guard reads.")
	cmd.Flags().BoolVar(&f.DryRun, "dry-run", false, "Print the dry run and open no pull request.")
	cmd.Flags().StringVarP(&f.Output, "output", "o", outputText, "Output format: text or json (the dry run and the pull request URL).")
}

func (f *flag) Validate() error {
	if f.Team == "" {
		return microerror.Maskf(invalidFlagError, "--team must not be empty")
	}
	if f.Name == "" {
		return microerror.Maskf(invalidFlagError, "--name must not be empty")
	}
	if f.Output != outputText && f.Output != outputJSON {
		return microerror.Maskf(invalidFlagError, "--output must be %s or %s", outputText, outputJSON)
	}
	if !strings.HasPrefix(f.Team, "team-") {
		f.Team = "team-" + f.Team
	}
	return nil
}

func oneOf(values []string) string {
	if len(values) == 0 {
		return "see the repositories schema"
	}
	return strings.Join(values, "|")
}
