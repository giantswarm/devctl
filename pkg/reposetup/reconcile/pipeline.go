package reconcile

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// CircleCIContextPrefix is the status context GitHub receives for a
// CircleCI job: "ci/circleci: <job name>".
const CircleCIContextPrefix = "ci/circleci: "

// AlignFilesBranch is the branch giantswarm/github's align-files action
// pushes a repository's generated files to and opens its pull request from.
const AlignFilesBranch = "teams-alignment-branch"

// AlignFilesLogin is the login of the align-files App, the author of the
// pull requests it opens from [AlignFilesBranch].
const AlignFilesLogin = "giantswarm-align-files[bot]"

// stateOpen is the state of an open pull request, as a list filters on it.
const stateOpen = "open"

// PipelineFiles are the files of a generated pipeline that carry jobs,
// relative to .circleci: workflows.yml is what `devctl gen circleci`
// writes, custom.yml the repository-owned additions the setup workflow in
// config.yml merges into it at pipeline runtime.
var PipelineFiles = []string{"workflows.yml", "custom.yml"}

// E2ETestsDir is the directory of a repository's end-to-end tests, which
// Heimdall runs on its pull requests.
const E2ETestsDir = "tests/e2e"

// HeimdallContext is the check run of Heimdall's gatekeeper, required once
// it has reported on a repository with an [E2ETestsDir].
const HeimdallContext = "Heimdall - PR Gatekeeper"

// GateContexts returns the status contexts of the branch-side jobs of the
// pipeline documents in files, sorted and without duplicates. A job counts
// unless its branch filter restricts it to named branches (`only:`) or
// ignores every branch (`ignore: /.*/`, the tag-only release jobs): those
// never report on a pull request and cannot gate one. Jobs of every workflow
// count; custom.yml may add its own workflows. A workflow that runs on a
// schedule (`triggers:`) or a condition (`when:`, `unless:`) and an
// approval job wait for something other than a push, so they cannot gate a
// pull request either.
func GateContexts(files ...[]byte) ([]string, error) {
	seen := map[string]bool{}
	for _, raw := range files {
		jobs, err := GateJobs(raw)
		if err != nil {
			return nil, err
		}
		for _, j := range jobs {
			seen[CircleCIContextPrefix+j] = true
		}
	}
	contexts := make([]string, 0, len(seen))
	for c := range seen {
		contexts = append(contexts, c)
	}
	sort.Strings(contexts)
	return contexts, nil
}

// GateJobs parses one CircleCI config document and returns the names of its
// branch-side jobs (see [GateContexts]).
func GateJobs(raw []byte) ([]string, error) {
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
		if !workflowGatesPushes(wfMap) {
			continue
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
					if !jobGatesBranches(p) || p["type"] == "approval" {
						continue
					}
					names = append(names, name)
				}
			}
		}
	}
	return names, nil
}

// workflowGatesPushes reports whether a workflow runs on every push: no
// schedule (`triggers:`) and no condition (`when:`, `unless:`).
func workflowGatesPushes(wf map[string]any) bool {
	for _, key := range []string{"triggers", "when", "unless"} {
		if _, has := wf[key]; has {
			return false
		}
	}
	return true
}

// jobGatesBranches reports whether a job with the given parameters runs on
// (unnamed) branches: no `filters.branches.only`, and no
// `filters.branches.ignore` that matches every branch.
func jobGatesBranches(params map[string]any) bool {
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

// StaleCircleCIContexts returns the required contexts of CircleCI jobs the
// pipeline no longer has: every "ci/circleci: <job>" context in existing
// whose job is not in live. Contexts of other systems are never returned.
func StaleCircleCIContexts(existing, live []string) []string {
	keep := make(map[string]bool, len(live))
	for _, c := range live {
		keep[c] = true
	}
	var stale []string
	for _, name := range existing {
		if strings.HasPrefix(name, CircleCIContextPrefix) && !keep[name] {
			stale = append(stale, name)
		}
	}
	return stale
}
