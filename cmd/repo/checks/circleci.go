package checks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v91/github"
	"gopkg.in/yaml.v3"
)

// circleCIContextPrefix is the status context GitHub receives for a CircleCI
// job: "ci/circleci: <job name>".
const circleCIContextPrefix = "ci/circleci: "

// circleCIPipelineFiles are the files of a generated pipeline that carry
// jobs: workflows.yml is what `devctl gen circleci` writes, custom.yml the
// repo-owned additions that the setup workflow in config.yml merges into it at
// pipeline runtime. Whatever is missing is skipped.
var circleCIPipelineFiles = []string{"workflows.yml", "custom.yml"}

// circleCIGateContexts returns the status contexts of the branch-side jobs of
// the CircleCI pipeline in dir, sorted and without duplicates, and whether any
// pipeline file was found. A job counts unless its branch filter restricts it
// to named branches (`only:`) or ignores every branch (`ignore: /.*/`, the
// tag-only release jobs): those never report on a pull request and cannot gate
// one. Jobs of every workflow count, custom.yml may add its own workflows.
func circleCIGateContexts(dir string) ([]string, bool, error) {
	seen := map[string]bool{}
	found := false
	for _, file := range circleCIPipelineFiles {
		p := filepath.Join(dir, file)
		raw, err := os.ReadFile(filepath.Clean(p)) //nolint:gosec // the directory comes from the operator's own flag
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, found, microerror.Mask(err)
		}
		found = true
		jobs, err := circleCIGateJobs(raw)
		if err != nil {
			return nil, found, microerror.Maskf(invalidConfigError, "%s: %v", p, err)
		}
		for _, j := range jobs {
			seen[circleCIContextPrefix+j] = true
		}
	}
	contexts := make([]string, 0, len(seen))
	for c := range seen {
		contexts = append(contexts, c)
	}
	sort.Strings(contexts)
	return contexts, found, nil
}

// circleCIGateJobs parses one CircleCI config document and returns the names
// of its branch-side jobs (see circleCIGateContexts).
func circleCIGateJobs(raw []byte) ([]string, error) {
	var doc struct {
		Workflows map[string]any `yaml:"workflows"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	var names []string
	for _, wf := range doc.Workflows {
		wfMap, ok := wf.(map[string]any)
		if !ok {
			continue // `version: 2` keys and the like
		}
		jobs, ok := wfMap["jobs"].([]any)
		if !ok {
			continue
		}
		for _, entry := range jobs {
			switch e := entry.(type) {
			case string:
				// A bare job name: no filters, runs on every branch.
				names = append(names, e)
			case map[string]any:
				// `{ "<orb/job>": { name, filters, ... } }`, params may be empty.
				for orbJob, params := range e {
					name := orbJob
					var p map[string]any
					if pm, ok := params.(map[string]any); ok {
						p = pm
					}
					if n, ok := p["name"].(string); ok && n != "" {
						name = n
					}
					if !circleCIJobGatesBranches(p) {
						continue
					}
					names = append(names, name)
				}
			}
		}
	}
	return names, nil
}

// circleCIJobGatesBranches reports whether a job with the given parameters
// runs on (unnamed) branches: no `filters.branches.only`, and no
// `filters.branches.ignore` that matches every branch.
func circleCIJobGatesBranches(params map[string]any) bool {
	filters, _ := params["filters"].(map[string]any)
	branches, _ := filters["branches"].(map[string]any)
	if branches == nil {
		return true
	}
	if _, has := branches["only"]; has {
		return false
	}
	switch ignore := branches["ignore"].(type) {
	case string:
		return ignore != "/.*/"
	case []any:
		for _, v := range ignore {
			if s, ok := v.(string); ok && s == "/.*/" {
				return false
			}
		}
	}
	return true
}

// staleCircleCIContexts returns the required contexts of CircleCI jobs that
// the pipeline no longer has: every "ci/circleci: <job>" context in existing
// whose job is not in live. Contexts of other systems (GitHub Actions
// workflows) are never touched.
func staleCircleCIContexts(existing []*github.RequiredStatusCheck, live []string) []string {
	keep := make(map[string]bool, len(live))
	for _, c := range live {
		keep[c] = true
	}
	var stale []string
	for _, c := range existing {
		name := c.GetContext()
		if strings.HasPrefix(name, circleCIContextPrefix) && !keep[name] {
			stale = append(stale, name)
		}
	}
	return stale
}

func describeContexts(contexts []string) string {
	if len(contexts) == 0 {
		return "(none)"
	}
	return fmt.Sprintf("%v", contexts)
}
