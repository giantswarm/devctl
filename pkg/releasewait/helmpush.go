package releasewait

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

// chartDirs are the directories a repository keeps its charts in: helm/ for
// the architect orb, charts/ for an upstream line's own layout.
var chartDirs = []string{helmDir, "charts"}

// HelmPushJob is a job a hand-written configuration defines itself whose
// commands push charts with helm (giantswarm/substrate's charts job): the
// workflow names it runs under, the OCI repository it pushes to and the
// words of its commands, which name the charts it packages.
type HelmPushJob struct {
	// Name is the job's name in the configuration's jobs.
	Name string
	// Names are the names the workflows run it under: its name parameter,
	// else the job's name.
	Names []string
	// Target is the repository path helm push names after oci://, with the
	// job's environment expanded; empty when a variable stays unresolved.
	Target string
	// Raw is the target as written.
	Raw string
	// words are the words of the job's commands.
	words map[string]bool
}

// mentions says whether the job's commands name the chart, by its directory
// or its name.
func (j HelmPushJob) mentions(dir, name string) bool {
	return j.words[dir] || j.words[name]
}

var (
	helmPushTarget = regexp.MustCompile(`helm\s+push\s+\S+\s+["']?oci://([^"'\s]+)`)
	shellVariable  = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)
)

// ParseHelmPushJobs reads the jobs of a CircleCI configuration that push
// charts with helm push to an OCI registry, in the configuration's order.
// The orb's jobs are not among them; ParsePushJobs reads those.
func ParseHelmPushJobs(config []byte) ([]HelmPushJob, error) {
	return parseHelmPushJobs(config, nil)
}

// parseHelmPushJobs reads the helm push jobs the way ParseHelmPushJobs does,
// and with makefile (nil for none) those whose commands run helm push
// through a make target of the root Makefile (giantswarm/kagent-upstream's
// make helm-publish): the first push the target runs names the job's target,
// with the make command line's variables over the Makefile's.
func parseHelmPushJobs(config []byte, makefile func() (*makefile, error)) ([]HelmPushJob, error) {
	var doc struct {
		Jobs      yaml.Node `yaml:"jobs"`
		Workflows yaml.Node `yaml:"workflows"`
	}
	if err := yaml.Unmarshal(config, &doc); err != nil {
		return nil, fmt.Errorf("parsing the CircleCI configuration: %w", err)
	}
	if doc.Jobs.Kind != yaml.MappingNode {
		return nil, nil
	}
	runs := workflowNames(doc.Workflows)
	var jobs []HelmPushJob
	for i := 0; i+1 < len(doc.Jobs.Content); i += 2 {
		name := doc.Jobs.Content[i].Value
		var def struct {
			Environment map[string]string `yaml:"environment"`
			Steps       []yaml.Node       `yaml:"steps"`
		}
		if err := doc.Jobs.Content[i+1].Decode(&def); err != nil {
			return nil, fmt.Errorf("parsing the job %s: %w", name, err)
		}
		commands := strings.Join(runCommands(def.Steps), "\n")
		env := jobEnvironment(def.Environment, commands)
		var raw string
		if match := helmPushTarget.FindStringSubmatch(commands); match != nil {
			raw = match[1]
		} else if makefile != nil && makeCall.MatchString(commands) {
			mk, err := makefile()
			if err != nil {
				return nil, err
			}
			for _, call := range makeInvocations(commands, env) {
				for _, target := range call.targets {
					raws, recipes := mk.pushTargets(target, call.vars)
					if len(raws) > 0 && raw == "" {
						raw = raws[0]
					}
					if len(raws) > 0 {
						commands += "\n" + strings.Join(recipes, "\n")
					}
				}
			}
		}
		if raw == "" {
			continue
		}
		job := HelmPushJob{Name: name, Names: runs[name], Raw: raw, Target: expand(raw, env), words: words(commands)}
		if len(job.Names) == 0 {
			job.Names = []string{name}
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

// workflowNames maps each job the workflows run to the names they run it
// under.
func workflowNames(workflows yaml.Node) map[string][]string {
	names := map[string][]string{}
	if workflows.Kind != yaml.MappingNode {
		return names
	}
	for i := 0; i+1 < len(workflows.Content); i += 2 {
		var wf struct {
			Jobs []yaml.Node `yaml:"jobs"`
		}
		if workflows.Content[i+1].Decode(&wf) != nil {
			continue
		}
		for _, item := range wf.Jobs {
			switch {
			case item.Kind == yaml.ScalarNode:
				names[item.Value] = append(names[item.Value], item.Value)
			case item.Kind == yaml.MappingNode && len(item.Content) >= 2:
				ref := item.Content[0].Value
				var params struct {
					Name string `yaml:"name"`
				}
				_ = item.Content[1].Decode(&params)
				if params.Name == "" {
					params.Name = ref
				}
				names[ref] = append(names[ref], params.Name)
			}
		}
	}
	return names
}

// runCommands are the commands of a job's run steps, in either form.
func runCommands(steps []yaml.Node) []string {
	var commands []string
	for _, step := range steps {
		var s struct {
			Run yaml.Node `yaml:"run"`
		}
		if step.Kind != yaml.MappingNode || step.Decode(&s) != nil {
			continue
		}
		switch s.Run.Kind {
		case yaml.ScalarNode:
			commands = append(commands, s.Run.Value)
		case yaml.MappingNode:
			var run struct {
				Command string `yaml:"command"`
			}
			if s.Run.Decode(&run) == nil {
				commands = append(commands, run.Command)
			}
		}
	}
	return commands
}

// expand replaces $VAR and ${VAR} with the job's environment; a variable the
// environment does not name (a context's, one the commands set) leaves the
// target unknown.
func expand(target string, env map[string]string) string {
	return strings.TrimSuffix(expandShell(target, env), "/")
}

func words(text string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(text, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' && r != '.'
	}) {
		out[w] = true
		// A package file names the chart before its version:
		// kagent-crds-$(VERSION).tgz.
		out[strings.TrimRight(w, "-.")] = true
	}
	return out
}

// tagChart is a chart directory at the tag and the name its Chart.yaml
// declares.
type tagChart struct{ dir, name string }

// chartTemplateFile is the chart a repository renders its Chart.yaml from
// before packaging (giantswarm/kagent-upstream's make helm-version): the
// name it declares is the published one, as a Chart.yaml's is.
const chartTemplateFile = "Chart-template.yaml"

// readTagCharts lists the charts at the tag: every directory under helm/ and
// charts/ with a Chart.yaml, else a Chart-template.yaml, named after it.
func readTagCharts(ctx context.Context, gh GitHub, owner, repo, sha string, content TagContent) ([]tagChart, error) {
	var charts []tagChart
	for _, base := range chartDirs {
		if !slices.Contains(content.Root, base) {
			continue
		}
		dirs, err := gh.ListDirectory(ctx, owner, repo, base, sha)
		if githubclient.IsNotFound(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("listing %s at %s: %w", base, short(sha), err)
		}
		slices.Sort(dirs)
		names := chartNamesIn(ctx, gh, owner, repo, sha, base, chartFile)
		templateNames := chartNamesIn(ctx, gh, owner, repo, sha, base, chartTemplateFile)
		for _, dir := range dirs {
			name, err := names(dir)
			if agentcli.Exit(err) == agentcli.ExitUsage {
				name, err = templateNames(dir)
			}
			if err == nil && strings.ContainsAny(name, "${}") {
				err = usageErr("%s/%s declares its name through a variable", base, dir)
			}
			if agentcli.Exit(err) == agentcli.ExitUsage {
				// A directory without a readable Chart.yaml is no chart.
				continue
			}
			if err != nil {
				return nil, err
			}
			charts = append(charts, tagChart{dir: base + "/" + dir, name: name})
		}
	}
	return charts, nil
}

// helmPushArtifacts are the charts the helm push jobs the tag pipeline runs
// push, at the repository path the job pushes to and the bare version, and
// warnings for the charts at the tag no artifact covers.
func helmPushArtifacts(charts []tagChart, jobs []HelmPushJob, pipelineJobs map[string]bool, covered []Artifact, version string, endpoints agentcli.Endpoints) ([]Artifact, []string) {
	var running []HelmPushJob
	for _, job := range jobs {
		if slices.ContainsFunc(job.Names, func(n string) bool { return pipelineJobs[n] }) {
			running = append(running, job)
		}
	}
	var artifacts []Artifact
	var warnings []string
	for _, chart := range charts {
		if slices.ContainsFunc(covered, func(a Artifact) bool { return a.Kind == KindChart && a.chart == chart.name }) {
			continue
		}
		i := slices.IndexFunc(running, func(j HelmPushJob) bool { return j.mentions(chart.dir[strings.Index(chart.dir, "/")+1:], chart.name) })
		if i < 0 {
			warnings = append(warnings, fmt.Sprintf("%s (chart %s) is unchecked: no push job of the tag pipeline names it; if the pipeline publishes it, name it with --chart", chart.dir, chart.name))
			continue
		}
		job := running[i]
		path, private, ok := registryPath(job.Target, endpoints)
		if !ok {
			warnings = append(warnings, fmt.Sprintf("%s (chart %s) is unchecked: the job %s pushes it to oci://%s, which names no repository of %s or %s once its environment is read; name it with --chart", chart.dir, chart.name, job.Name, job.Raw, endpoints.RegistryPublic, endpoints.RegistryPrivate))
			continue
		}
		artifacts = append(artifacts, namedChartArtifact(path+"/"+chart.name, version, private, endpoints))
	}
	return artifacts, warnings
}

// registryPath splits a helm push target into the repository path in one of
// the registries the wait probes and whether it is the private one; the
// configuration names the production hosts, which the endpoints may move.
func registryPath(target string, endpoints agentcli.Endpoints) (path string, private, ok bool) {
	if target == "" {
		return "", false, false
	}
	defaults := agentcli.DefaultEndpoints()
	for _, r := range []struct {
		hosts   []string
		private bool
	}{
		{[]string{endpoints.RegistryPublic, defaults.RegistryPublic}, false},
		{[]string{endpoints.RegistryPrivate, defaults.RegistryPrivate}, true},
	} {
		for _, host := range r.hosts {
			if rest, ok := strings.CutPrefix(target, host+"/"); ok && rest != "" {
				return rest, r.private, true
			}
		}
	}
	return "", false, false
}
