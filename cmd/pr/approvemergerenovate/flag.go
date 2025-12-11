package approvemergerenovate

import (
	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"
)

type flag struct {
	Query       string
	DryRun      bool
	MergeMethod string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&f.Query, "query", "q", "", "Search query for Renovate PRs (required)")
	cmd.Flags().BoolVar(&f.DryRun, "dry-run", false, "Only show what would be done without making changes")
	cmd.Flags().StringVar(&f.MergeMethod, "merge-method", "", "Override merge method (merge, squash, rebase). If not set, uses repository's default.")
}

func (f *flag) Validate() error {
	if f.Query == "" {
		return microerror.Maskf(invalidFlagsError, "--query flag is required")
	}

	// Only validate merge method if it's set
	if f.MergeMethod != "" {
		validMergeMethods := map[string]bool{
			"merge":  true,
			"squash": true,
			"rebase": true,
		}
		if !validMergeMethods[f.MergeMethod] {
			return microerror.Maskf(invalidFlagsError, "--merge-method must be one of: merge, squash, rebase")
		}
	}

	return nil
}

