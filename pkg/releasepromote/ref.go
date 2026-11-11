package releasepromote

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

var maintenanceRef = regexp.MustCompile(`^release-([0-9]+\.[0-9]+)$`)

// checkRef returns the maintenance series, empty for the default branch.
// Only --ref opts into this preflight; existing calls retain their behavior.
func checkRef(ctx context.Context, config Config, repository string) (string, error) {
	owner, repo, ok := SplitRepository(repository)
	if !ok {
		return "", usageErr("%q is not a repository: expected owner/repo", repository)
	}
	branch, err := config.GitHub.DefaultBranch(ctx, owner, repo)
	if err != nil {
		return "", agentcli.NewExitError(agentcli.ExitRed, agentcli.VerdictRed, "reading the repository: %v", githubclient.ExplainNotFound(err, config.NotFoundHint))
	}
	if config.Ref == branch {
		return "", nil
	}
	series := maintenanceRef.FindStringSubmatch(config.Ref)
	if series == nil {
		return "", usageErr("--ref %q must name the default branch %q or a release-X.Y branch enabled by %s", config.Ref, branch, Workflow)
	}
	workflow, found, err := config.GitHub.ReadFile(ctx, owner, repo, workflowPath, config.Ref)
	if err != nil {
		return "", agentcli.NewExitError(agentcli.ExitRed, agentcli.VerdictRed, "reading %s on %s: %v", workflowPath, config.Ref, err)
	}
	if !found {
		return "", usageErr("--ref %q has no %s", config.Ref, workflowPath)
	}
	enabled, err := workflowReleasesBranch(workflow, config.Ref)
	if err != nil {
		return "", usageErr("checking --ref %q against %s: %v", config.Ref, workflowPath, err)
	}
	if !enabled {
		return "", usageErr("--ref %q is not enabled by the push and workflow_dispatch triggers of %s", config.Ref, workflowPath)
	}
	return series[1], nil
}

func workflowReleasesBranch(workflow []byte, branch string) (bool, error) {
	var wf struct {
		On map[string]yaml.Node `yaml:"on"`
	}
	if err := yaml.Unmarshal(workflow, &wf); err != nil {
		return false, err
	}
	push, ok := wf.On["push"]
	if !ok {
		return false, nil
	}
	if _, ok := wf.On["workflow_dispatch"]; !ok {
		return false, nil
	}
	var filters struct {
		Branches       []string `yaml:"branches"`
		BranchesIgnore []string `yaml:"branches-ignore"`
		Tags           []string `yaml:"tags"`
		TagsIgnore     []string `yaml:"tags-ignore"`
	}
	if err := push.Decode(&filters); err != nil {
		return false, err
	}
	if filters.Branches != nil && filters.BranchesIgnore != nil {
		return false, fmt.Errorf("push declares both branches and branches-ignore")
	}
	if filters.Branches != nil {
		return matchesBranches(filters.Branches, branch)
	}
	if filters.BranchesIgnore != nil {
		ignored, err := matchesBranches(filters.BranchesIgnore, branch)
		return !ignored, err
	}
	// With only tag filters, GitHub does not run on branch pushes.
	return filters.Tags == nil && filters.TagsIgnore == nil, nil
}

// matchesBranches applies GitHub's ordered branch filters, including the
// [0-9]+ ranges in the generated maintenance trigger. A shell glob treats
// '+' literally and would refuse the very branches that workflow enables.
// https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#filter-pattern-cheat-sheet
func matchesBranches(patterns []string, branch string) (bool, error) {
	matched := false
	for _, pattern := range patterns {
		negative := strings.HasPrefix(pattern, "!")
		pattern = strings.TrimPrefix(pattern, "!")
		var expression strings.Builder
		expression.WriteString("^")
		for i := 0; i < len(pattern); i++ {
			switch pattern[i] {
			case '*':
				if i+1 < len(pattern) && pattern[i+1] == '*' {
					expression.WriteString(".*")
					i++
				} else {
					expression.WriteString("[^/]*")
				}
			case '?', '+':
				expression.WriteByte(pattern[i])
			case '[':
				end := strings.IndexByte(pattern[i:], ']')
				if end < 0 || strings.ContainsAny(pattern[i+1:i+end], `^!\[:`) {
					return false, fmt.Errorf("invalid branch filter %q", pattern)
				}
				expression.WriteString(pattern[i : i+end+1])
				i += end
			case '\\':
				if i+1 == len(pattern) {
					return false, fmt.Errorf("invalid branch filter %q", pattern)
				}
				i++
				expression.WriteString(regexp.QuoteMeta(pattern[i : i+1]))
			default:
				expression.WriteString(regexp.QuoteMeta(pattern[i : i+1]))
			}
		}
		expression.WriteString("$")
		compiled, err := regexp.Compile(expression.String())
		if err != nil {
			return false, fmt.Errorf("invalid branch filter %q: %w", pattern, err)
		}
		if compiled.MatchString(branch) {
			matched = !negative
		}
	}
	return matched, nil
}
