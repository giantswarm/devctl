package releasewait

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// checkAutoReleaseBranch rules out a tag only when every auto-release
// workflow at the merge commit excludes pushes to the pull request's base.
// Existing tags are resolved before this check, including manually cut tags.
func (w *Waiter) checkAutoReleaseBranch(ctx context.Context, sha string, files []string) error {
	var triggers []string
	for _, name := range files {
		file := path.Join(workflowsDir, name)
		content, err := w.config.GitHub.GetFile(ctx, w.config.Owner, w.config.Repo, file, sha)
		if err != nil {
			return fmt.Errorf("reading %s at %s: %w", file, short(sha), err)
		}
		filter, err := readPushBranches(content.Data)
		if err != nil {
			return fmt.Errorf("reading %s at %s: %w", file, short(sha), err)
		}
		// An unrestricted push needs no further pull request read when the
		// caller already supplied the merge commit.
		if filter.enabled && filter.Branches == nil && filter.BranchesIgnore == nil && filter.Tags == nil && filter.TagsIgnore == nil {
			return nil
		}
		base, err := w.pullRequestBase(ctx)
		if err != nil {
			return err
		}
		matches, err := filter.matches(base)
		if err != nil {
			return fmt.Errorf("reading %s at %s: %w", file, short(sha), err)
		}
		if matches {
			return nil
		}
		triggers = append(triggers, fmt.Sprintf("%s (%s)", name, filter.description()))
	}
	return noReleaseErr("the auto-release workflows of %s/%s at %s trigger on %s; nothing tags a merge into %s", w.config.Owner, w.config.Repo, short(sha), strings.Join(triggers, "; "), w.baseRef)
}

func (w *Waiter) pullRequestBase(ctx context.Context) (string, error) {
	if w.baseRef == "" {
		merge, err := w.config.GitHub.GetPullRequestMerge(ctx, w.config.Owner, w.config.Repo, w.config.PR)
		if err != nil {
			return "", fmt.Errorf("reading the base of pull request #%d: %w", w.config.PR, err)
		}
		w.baseRef = merge.BaseRef
	}
	if w.baseRef == "" {
		return "", usageErr("pull request #%d has no base branch: cannot check the auto-release push filters", w.config.PR)
	}
	return w.baseRef, nil
}

// pushEvent is the workflow trigger and run event of a pushed commit.
const pushEvent = "push"

// Path filters do not prove that a branch never releases, so only the ref
// filters are considered here.
type pushBranchFilter struct {
	Branches       []string `yaml:"branches"`
	BranchesIgnore []string `yaml:"branches-ignore"`
	Tags           []string `yaml:"tags"`
	TagsIgnore     []string `yaml:"tags-ignore"`
	enabled        bool
}

func readPushBranches(content []byte) (pushBranchFilter, error) {
	var doc struct {
		On yaml.Node `yaml:"on"`
	}
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return pushBranchFilter{}, usageErr("invalid auto-release workflow: %s", err)
	}
	var filter pushBranchFilter
	switch doc.On.Kind {
	case yaml.ScalarNode:
		filter.enabled = doc.On.Value == pushEvent
	case yaml.SequenceNode:
		for _, event := range doc.On.Content {
			filter.enabled = filter.enabled || event.Value == pushEvent
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(doc.On.Content); i += 2 {
			if doc.On.Content[i].Value != pushEvent {
				continue
			}
			trigger := doc.On.Content[i+1]
			if err := trigger.Decode(&filter); err != nil {
				return pushBranchFilter{}, usageErr("invalid auto-release push filters: %s", err)
			}
			filter.enabled = true
		}
	}
	if filter.Branches != nil && filter.BranchesIgnore != nil {
		return pushBranchFilter{}, usageErr("auto-release push filters declare both branches and branches-ignore")
	}
	return filter, nil
}

func (f pushBranchFilter) matches(base string) (bool, error) {
	if !f.enabled || (f.Branches == nil && f.BranchesIgnore == nil && (f.Tags != nil || f.TagsIgnore != nil)) {
		return false, nil
	}
	if f.Branches != nil {
		return matchPushBranches(f.Branches, base)
	}
	ignored, err := matchPushBranches(f.BranchesIgnore, base)
	return !ignored, err
}

func (f pushBranchFilter) description() string {
	switch {
	case !f.enabled:
		return "no push trigger"
	case f.Branches != nil:
		return "branches: " + strings.Join(f.Branches, ", ")
	case f.BranchesIgnore != nil:
		return "branches-ignore: " + strings.Join(f.BranchesIgnore, ", ")
	}
	return "tags only"
}

// GitHub checks patterns in order: a negative match excludes a previous
// match, and a later positive match includes it again.
func matchPushBranches(patterns []string, base string) (bool, error) {
	matched := false
	for _, pattern := range patterns {
		positive := !strings.HasPrefix(pattern, "!")
		expression, err := pushBranchPattern(strings.TrimPrefix(pattern, "!"))
		if err != nil {
			return false, err
		}
		if expression.MatchString(base) {
			matched = positive
		}
	}
	return matched, nil
}

// Actions filters differ from shell globs: ? and + repeat the preceding
// character or range. See GitHub's workflow-syntax filter pattern cheat sheet.
func pushBranchPattern(pattern string) (*regexp.Regexp, error) {
	var expression strings.Builder
	expression.WriteString("^")
	runes := []rune(pattern)
	for i := 0; i < len(runes); i++ {
		switch runes[i] {
		case '*':
			if i+1 < len(runes) && runes[i+1] == '*' {
				i++
				if i+1 < len(runes) && runes[i+1] == '/' && (i == 1 || runes[i-2] == '/') {
					expression.WriteString("(.*/)?")
					i++
				} else {
					expression.WriteString(".*")
				}
			} else {
				expression.WriteString("[^/]*")
			}
		case '?', '+':
			expression.WriteRune(runes[i])
		case '[':
			end := i + 1
			for end < len(runes) && runes[end] != ']' {
				c := runes[end]
				if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' {
					return nil, usageErr("invalid auto-release branch pattern %q", pattern)
				}
				end++
			}
			if end == len(runes) {
				return nil, usageErr("invalid auto-release branch pattern %q", pattern)
			}
			expression.WriteString(string(runes[i : end+1]))
			i = end
		case '\\':
			i++
			if i == len(runes) {
				return nil, usageErr("invalid auto-release branch pattern %q", pattern)
			}
			expression.WriteString(regexp.QuoteMeta(string(runes[i])))
		default:
			expression.WriteString(regexp.QuoteMeta(string(runes[i])))
		}
	}
	expression.WriteString("$")
	compiled, err := regexp.Compile(expression.String())
	if err != nil {
		return nil, usageErr("invalid auto-release branch pattern %q: %s", pattern, err)
	}
	return compiled, nil
}
