// Package circleciconfig reads a repository's CircleCI configuration for the
// jobs CircleCI runs on a branch or a tag, so a wait that reads CircleCI only
// through the commit statuses it posts to GitHub knows the jobs that have
// posted nothing yet: a job behind requires: posts its first status when it
// starts, after the jobs it requires have finished.
//
// The configuration is read as CircleCI reads it: devctl's generated setup
// workflow continues with .circleci/workflows.yml deep-merged with the
// optional .circleci/custom.yml (yq's *+: maps merge, lists append), any
// other configuration is its own. A job's status context is
// "ci/circleci: <name>", the name its workflow entry gives it or else the
// job's own.
package circleciconfig

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// The files the configuration is read from.
const (
	ConfigPath    = ".circleci/config.yml"
	WorkflowsPath = ".circleci/workflows.yml"
	CustomPath    = ".circleci/custom.yml"
)

// Reader reads a file of the revision the jobs are listed for; found is
// false when the revision has no such file.
type Reader func(ctx context.Context, path string) (content []byte, found bool, err error)

// Run is what a pipeline runs for: a branch, or a tag.
type Run struct {
	Branch string
	Tag    string
}

func (r Run) String() string {
	if r.Tag != "" {
		return "tag " + r.Tag
	}
	return "branch " + r.Branch
}

// Jobs are the jobs CircleCI runs for one revision.
type Jobs struct {
	// Names are the names the jobs post their statuses under, sorted.
	Names []string
	// Warnings name what the configuration holds that Names cannot account
	// for: a workflow whose condition is not known before the pipeline runs,
	// a matrix job, a continuation devctl does not generate.
	Warnings []string
}

// List lists the jobs CircleCI runs for run from the configuration read
// reads: found is false when the revision carries no ConfigPath.
func List(ctx context.Context, read Reader, run Run) (Jobs, bool, error) {
	content, found, err := read(ctx, ConfigPath)
	if err != nil || !found {
		return Jobs{}, found, err
	}
	config, err := parse(ConfigPath, content)
	if err != nil {
		return Jobs{}, true, err
	}
	var jobs Jobs
	names := map[string]bool{}
	jobs.add(names, config, run)
	if setup, _ := config["setup"].(bool); setup {
		continued, err := continuation(ctx, read, content)
		if err != nil {
			return Jobs{}, true, err
		}
		if continued == nil {
			jobs.Warnings = append(jobs.Warnings, fmt.Sprintf("%s is a setup workflow that continues with a configuration devctl does not generate: the continued jobs count once they post a status", ConfigPath))
		} else if len(names) > 0 {
			// The continuation runs only after a setup job ran.
			jobs.add(names, continued, run)
		}
	}
	for name := range names {
		jobs.Names = append(jobs.Names, name)
	}
	sort.Strings(jobs.Names)
	return jobs, true, nil
}

// continuation is the configuration devctl's generated setup workflow
// continues with, nil for another setup workflow.
func continuation(ctx context.Context, read Reader, setup []byte) (map[string]any, error) {
	if !strings.Contains(string(setup), WorkflowsPath) {
		return nil, nil
	}
	content, found, err := read(ctx, WorkflowsPath)
	if err != nil || !found {
		return nil, err
	}
	merged, err := parse(WorkflowsPath, content)
	if err != nil {
		return nil, err
	}
	content, found, err = read(ctx, CustomPath)
	if err != nil {
		return nil, err
	}
	if found {
		custom, err := parse(CustomPath, content)
		if err != nil {
			return nil, err
		}
		merged, _ = deepMerge(merged, custom).(map[string]any)
	}
	return merged, nil
}

func parse(path string, content []byte) (map[string]any, error) {
	var config map[string]any
	if err := yaml.Unmarshal(content, &config); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return config, nil
}

// deepMerge is yq's *+ of b onto a: maps merge, lists append, b's scalar
// wins.
func deepMerge(a, b any) any {
	switch b := b.(type) {
	case map[string]any:
		am, ok := a.(map[string]any)
		if !ok {
			return b
		}
		out := make(map[string]any, len(am)+len(b))
		for k, v := range am {
			out[k] = v
		}
		for k, v := range b {
			out[k] = deepMerge(am[k], v)
		}
		return out
	case []any:
		al, ok := a.([]any)
		if !ok {
			return b
		}
		return append(append([]any{}, al...), b...)
	}
	return b
}

// job is one entry of a workflow's job list.
type job struct {
	name     string
	requires []string
	// approval: the job is an approval, which posts no status.
	approval bool
}

// add adds the jobs config runs for run to names.
func (j *Jobs) add(names map[string]bool, config map[string]any, run Run) {
	definitions, _ := config["jobs"].(map[string]any)
	workflows, _ := config["workflows"].(map[string]any)
	keys := make([]string, 0, len(workflows))
	for key := range workflows {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		workflow, ok := workflows[key].(map[string]any)
		if !ok {
			// "version: 2" sits among the workflows of a 2.0 configuration.
			continue
		}
		if _, scheduled := workflow["triggers"]; scheduled {
			continue
		}
		runs, known := condition(workflow, config, run)
		if !known {
			j.Warnings = append(j.Warnings, fmt.Sprintf("workflow %s runs on a condition known only once the pipeline runs: its jobs count once they post a status", key))
			continue
		}
		if !runs {
			continue
		}
		entries, _ := workflow["jobs"].([]any)
		var listed []job
		for _, entry := range entries {
			ref, params := jobEntry(entry)
			if ref == "" {
				continue
			}
			definition, _ := definitions[ref].(map[string]any)
			name := ref
			if n, ok := params["name"].(string); ok && n != "" {
				name = n
			}
			if _, matrix := params["matrix"]; matrix {
				j.Warnings = append(j.Warnings, fmt.Sprintf("workflow %s runs %s as a matrix: its jobs count once they post a status", key, name))
				continue
			}
			if !filtered(params["filters"], run) {
				continue
			}
			// An approval posts no status; the jobs it holds do.
			approval := params["type"] == "approval" || definition["type"] == "approval"
			listed = append(listed, job{name: name, requires: requires(params["requires"]), approval: approval})
		}
		for _, job := range runnable(listed) {
			names[job] = true
		}
	}
}

// jobEntry is a workflow job list's entry: the job it runs and its
// parameters.
func jobEntry(entry any) (string, map[string]any) {
	switch entry := entry.(type) {
	case string:
		return entry, nil
	case map[string]any:
		for ref, params := range entry {
			p, _ := params.(map[string]any)
			return ref, p
		}
	}
	return "", nil
}

// requires are the names of the jobs a requires: list names, in its list
// form or its map form with the statuses each must end in.
func requires(value any) []string {
	var names []string
	switch value := value.(type) {
	case []any:
		for _, item := range value {
			switch item := item.(type) {
			case string:
				names = append(names, item)
			case map[string]any:
				for name := range item {
					names = append(names, name)
				}
			}
		}
	case map[string]any:
		for name := range value {
			names = append(names, name)
		}
	}
	return names
}

// runnable are the names of the jobs that post a status and whose required
// jobs all run: a job behind one the run filters out never starts.
func runnable(jobs []job) []string {
	listed := map[string]job{}
	for _, job := range jobs {
		listed[job.name] = job
	}
	memo := map[string]bool{}
	var runs func(name string, seen map[string]bool) bool
	runs = func(name string, seen map[string]bool) bool {
		if v, ok := memo[name]; ok {
			return v
		}
		job, ok := listed[name]
		if !ok || seen[name] {
			return false
		}
		seen[name] = true
		ok = true
		for _, r := range job.requires {
			ok = ok && runs(r, seen)
		}
		memo[name] = ok
		return ok
	}
	var names []string
	for _, job := range jobs {
		if !job.approval && runs(job.name, map[string]bool{}) {
			names = append(names, job.name)
		}
	}
	return names
}

// filtered says whether a job with filters runs for run, as CircleCI decides
// it: a branch runs every job its branches filter does not exclude, a tag
// only the jobs whose tags filter includes it, whatever their branches
// filter says.
func filtered(filters any, run Run) bool {
	f, _ := filters.(map[string]any)
	if run.Tag != "" {
		tags, _ := f["tags"].(map[string]any)
		only, hasOnly := tags["only"]
		ignore, hasIgnore := tags["ignore"]
		if !hasOnly && !hasIgnore || hasOnly && !matches(only, run.Tag) {
			return false
		}
		return !matches(ignore, run.Tag)
	}
	branches, _ := f["branches"].(map[string]any)
	if only, ok := branches["only"]; ok && !matches(only, run.Branch) {
		return false
	}
	return !matches(branches["ignore"], run.Branch)
}

// matches says whether value matches one of patterns, a string or a list of
// them: /regexp/ matches the whole value, anything else is the value itself.
func matches(patterns any, value string) bool {
	var list []string
	switch p := patterns.(type) {
	case string:
		list = []string{p}
	case []any:
		for _, item := range p {
			if s, ok := item.(string); ok {
				list = append(list, s)
			}
		}
	}
	for _, p := range list {
		if len(p) > 1 && strings.HasPrefix(p, "/") && strings.HasSuffix(p, "/") {
			re, err := regexp.Compile("^(?:" + p[1:len(p)-1] + ")$")
			if err == nil && re.MatchString(value) {
				return true
			}
			continue
		}
		if p == value {
			return true
		}
	}
	return false
}

// condition evaluates a workflow's when: or unless: for a pipeline the push
// of run triggers: runs, and known false when it depends on what only the
// pipeline knows.
func condition(workflow, config map[string]any, run Run) (runs, known bool) {
	if when, ok := workflow["when"]; ok {
		return evaluate(when, config, run)
	}
	if unless, ok := workflow["unless"]; ok {
		v, known := evaluate(unless, config, run)
		return !v, known
	}
	return true, true
}

// pipelineValue matches a << pipeline.… >> value as a whole.
var pipelineValue = regexp.MustCompile(`^<<\s*pipeline\.([A-Za-z0-9_.-]+)\s*>>$`)

// value is what a logic statement's operand is for a pipeline the push of
// run triggers.
func value(v any, config map[string]any, run Run) (any, bool) {
	s, ok := v.(string)
	if !ok {
		return v, true
	}
	m := pipelineValue.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		if strings.Contains(s, "<<") {
			return nil, false
		}
		return s, true
	}
	switch key := m[1]; {
	case key == "trigger_source":
		return "webhook", true
	case key == "git.branch":
		return run.Branch, true
	case key == "git.tag":
		return run.Tag, true
	case strings.HasPrefix(key, "parameters."):
		parameters, _ := config["parameters"].(map[string]any)
		parameter, _ := parameters[strings.TrimPrefix(key, "parameters.")].(map[string]any)
		if d, ok := parameter["default"]; ok {
			return d, true
		}
	}
	return nil, false
}

// The logic statements that combine others.
const (
	opAnd = "and"
	opOr  = "or"
)

// evaluate evaluates a logic statement: a value, or and, or, not, equal and
// matches over them.
func evaluate(statement any, config map[string]any, run Run) (bool, bool) {
	m, ok := statement.(map[string]any)
	if !ok || len(m) != 1 {
		v, known := value(statement, config, run)
		return truthy(v), known
	}
	for op, arg := range m {
		args, _ := arg.([]any)
		switch op {
		case "not":
			v, known := evaluate(arg, config, run)
			return !v, known
		case opAnd, opOr:
			result := op == opAnd
			for _, a := range args {
				v, known := evaluate(a, config, run)
				if !known {
					return false, false
				}
				if op == opAnd {
					result = result && v
				} else {
					result = result || v
				}
			}
			return result, true
		case "equal":
			var first any
			for i, a := range args {
				v, known := value(a, config, run)
				if !known {
					return false, false
				}
				if i == 0 {
					first = v
				} else if fmt.Sprint(v) != fmt.Sprint(first) {
					return false, true
				}
			}
			return true, true
		case "matches":
			args, _ := arg.(map[string]any)
			pattern, _ := args["pattern"].(string)
			v, known := value(args["value"], config, run)
			if !known {
				return false, false
			}
			re, err := regexp.Compile(pattern)
			if err != nil {
				return false, false
			}
			return re.MatchString(fmt.Sprint(v)), true
		}
	}
	return false, false
}

// truthy is CircleCI's truth of a value: false, null, 0, NaN and the empty
// string are false.
func truthy(v any) bool {
	switch v := v.(type) {
	case nil:
		return false
	case bool:
		return v
	case int:
		return v != 0
	case float64:
		return v != 0
	case string:
		return v != ""
	}
	return true
}
