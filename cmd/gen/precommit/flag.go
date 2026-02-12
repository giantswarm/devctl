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
)

var allowedFlavors = map[string]bool{
	"bash":      true,
	"md":        true,
	"helmchart": true,
}

type flag struct {
	Language string
	Flavors  []string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&f.Language, flagLanguage, "l", "", "Language for pre-commit hooks, e.g. go, generic.")
	cmd.Flags().StringSliceVarP(&f.Flavors, flagFlavors, "f", []string{}, fmt.Sprintf("Comma-separated list of additional checker flavors (%s).", strings.Join(allowedFlavorsList(), ", ")))
}

func (f *flag) Validate() error {
	if f.Language == "" {
		return microerror.Maskf(invalidFlagError, "--%s cannot be empty", flagLanguage)
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
