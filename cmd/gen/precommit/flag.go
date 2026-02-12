package precommit

import (
	"fmt"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"
)

const (
	flagLanguage = "language"
	flagFlavors  = "flavors"
	flagRepoName = "repo-name"
)

var allowedFlavors = map[string]bool{
	"bash":      true,
	"md":        true,
	"helmchart": true,
}

type flag struct {
	Language string
	Flavors  []string
	RepoName string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&f.Language, flagLanguage, "l", "", "Language for pre-commit hooks, e.g. go, generic.")
	cmd.Flags().StringSliceVarP(&f.Flavors, flagFlavors, "f", []string{}, fmt.Sprintf("Comma-separated list of additional checker flavors (%s).", strings.Join(allowedFlavorsList(), ", ")))
	cmd.Flags().StringVarP(&f.RepoName, flagRepoName, "r", "", "Repository name under giantswarm organization (e.g. devctl).")
}

func (f *flag) Validate() error {
	if f.Language == "" {
		return microerror.Maskf(invalidFlagError, "--%s cannot be empty", flagLanguage)
	}

	if f.RepoName == "" {
		return microerror.Maskf(invalidFlagError, "--%s cannot be empty", flagRepoName)
	}

	for _, flavor := range f.Flavors {
		if !allowedFlavors[flavor] {
			return microerror.Maskf(invalidFlagError, "--%s contains invalid value %q, must be one of <%s>", flagFlavors, flavor, strings.Join(allowedFlavorsList(), "|"))
		}
	}

	return nil
}

func allowedFlavorsList() []string {
	result := make([]string, 0, len(allowedFlavors))
	for k := range allowedFlavors {
		result = append(result, k)
	}
	return result
}
