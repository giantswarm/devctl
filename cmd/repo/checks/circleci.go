package checks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v92/github"

	"github.com/giantswarm/devctl/v8/pkg/reposetup/reconcile"
)

// circleCIGateContexts returns the status contexts of the branch-side jobs of
// the CircleCI pipeline in dir (reconcile.PipelineFiles, whatever is missing
// is skipped), and whether any pipeline file was found. The rule is
// reconcile.GateContexts'.
func circleCIGateContexts(dir string) ([]string, bool, error) {
	var files [][]byte
	for _, file := range reconcile.PipelineFiles {
		p := filepath.Join(dir, file)
		raw, err := os.ReadFile(filepath.Clean(p)) //nolint:gosec // the directory comes from the operator's own flag
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, len(files) > 0, microerror.Mask(err)
		}
		if _, err := reconcile.GateJobs(raw); err != nil {
			return nil, true, microerror.Maskf(invalidConfigError, "%s: %v", p, err)
		}
		files = append(files, raw)
	}
	if len(files) == 0 {
		return nil, false, nil
	}
	contexts, err := reconcile.GateContexts(files...)
	if err != nil {
		return nil, true, microerror.Mask(err)
	}
	return contexts, true, nil
}

// staleCircleCIContexts returns the required contexts of CircleCI jobs that
// the pipeline no longer has (reconcile.StaleCircleCIContexts over the
// contexts of existing).
func staleCircleCIContexts(existing []*github.RequiredStatusCheck, live []string) []string {
	names := make([]string, 0, len(existing))
	for _, c := range existing {
		names = append(names, c.GetContext())
	}
	return reconcile.StaleCircleCIContexts(names, live)
}

func describeContexts(contexts []string) string {
	if len(contexts) == 0 {
		return "(none)"
	}
	return fmt.Sprintf("%v", contexts)
}
