package batchci

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const yamlMapTag = "!!map"

// These validation workflows are expensive. PR-specific automations, title
// checks, release workflows and publishing workflows deliberately stay intact.
var expensive = map[string]bool{
	"pre-commit": true, "chart_tests": true, "check_values_schema": true,
	"check_readme_links": true, "gitleaks": true, "analyze_github_actions": true,
	"cluster_app_documentation_validation":       true,
	"cluster_app_schema_validation":              true,
	"cluster_app_values_validation_using_schema": true,
	"test-kyverno-policies-with-chainsaw":        true,
}

func Apply(path string, content []byte, c *Config) ([]byte, error) {
	if c == nil {
		return content, nil
	}
	path = filepath.ToSlash(filepath.Clean(path))
	if strings.HasSuffix(path, ".circleci/config.yml") {
		return circleSetup(content, c)
	}
	if !strings.Contains(path, ".github/workflows/") {
		return content, nil
	}
	name := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "zz_generated."), filepath.Ext(path))
	if !expensive[name] {
		return content, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, err
	}
	root := doc.Content[0]
	on := field(root, "on")
	if on == nil {
		return nil, fmt.Errorf("batch CI: %s has no triggers", path)
	}
	if on.Kind == yaml.SequenceNode {
		m := &yaml.Node{Kind: yaml.MappingNode, Tag: yamlMapTag}
		for _, v := range on.Content {
			set(m, v.Value, &yaml.Node{Kind: yaml.MappingNode, Tag: yamlMapTag})
		}
		*on = *m
	}
	if on.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("batch CI: unsupported triggers in %s", path)
	}
	set(on, "merge_group", &yaml.Node{Kind: yaml.MappingNode, Tag: yamlMapTag})
	if c.Mode == modeBatch {
		remove(on, "pull_request")
	}
	if name == "check_readme_links" {
		jobs := field(root, "jobs")
		if jobs != nil {
			job := field(jobs, "check-readme-links")
			if job != nil {
				with := field(job, "with")
				if with != nil {
					set(with, "fail_on_error", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "${{ github.event_name == 'pull_request' || github.event_name == 'merge_group' }}"})
				}
			}
		}
	}
	// A queue must run every required check, even when the PR path filter
	// would omit it. Push/schedule/tag triggers and concurrency stay intact.
	return encode(&doc)
}

func field(n *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}
func set(n *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			n.Content[i+1] = value
			return
		}
	}
	n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}
func remove(n *yaml.Node, key string) {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			n.Content = append(n.Content[:i], n.Content[i+2:]...)
			return
		}
	}
}
func encode(n *yaml.Node) ([]byte, error) {
	var out bytes.Buffer
	e := yaml.NewEncoder(&out)
	e.SetIndent(2)
	if err := e.Encode(n); err != nil {
		return nil, err
	}
	if err := e.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func circleSetup(content []byte, c *Config) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, err
	}
	jobs := field(doc.Content[0], "jobs")
	if jobs == nil {
		return nil, fmt.Errorf("batch CI: setup has no jobs")
	}
	setup := field(jobs, "setup")
	if setup == nil {
		return nil, fmt.Errorf("batch CI: setup job missing")
	}
	steps := field(setup, "steps")
	if steps == nil {
		return nil, fmt.Errorf("batch CI: setup steps missing")
	}
	// checkout uses CIRCLE_SHA1. Verify it before custom continuation is read;
	// never fetch/reset a queue checkout to the moving base branch.
	command := `set -euo pipefail
if [[ "${CIRCLE_BRANCH:-}" == gh-readonly-queue/* ]]; then
  test -n "${CIRCLE_SHA1:-}"
  test "$(git rev-parse HEAD)" = "$CIRCLE_SHA1"
fi
`
	if c.Mode == modeBatch {
		command += `# Only supersedable PR work is suppressed. Queue, base, maintenance
# branches, tags and pipelines without PR metadata always continue.
if [[ -n "${CIRCLE_PULL_REQUEST:-}${CIRCLE_PULL_REQUESTS:-}" && -z "${CIRCLE_TAG:-}" && "${CIRCLE_BRANCH:-}" != gh-readonly-queue/* && "${CIRCLE_BRANCH:-}" != release-* && "${CIRCLE_BRANCH:-}" != ` + shellQuote(c.BaseBranch) + ` ]]; then
  circleci-agent step halt
fi
`
	}
	var step yaml.Node
	// Marshal the command as data, never interpolate repository strings into YAML.
	data, err := yaml.Marshal(map[string]interface{}{"run": map[string]string{nameKey: "Batch CI checkout and admission", "command": command}})
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, &step); err != nil {
		return nil, err
	}
	// Put the guard directly after checkout, ahead of both single- and
	// multi-image continuation, preserving custom merge, contexts and filters.
	index := -1
	for i, s := range steps.Content {
		if s.Kind == yaml.ScalarNode && s.Value == "checkout" {
			index = i + 1
			break
		}
	}
	if index < 0 {
		return nil, fmt.Errorf("batch CI: setup checkout missing")
	}
	steps.Content = append(steps.Content, nil)
	copy(steps.Content[index+1:], steps.Content[index:])
	steps.Content[index] = step.Content[0]
	return encode(&doc)
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
