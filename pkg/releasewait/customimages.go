package releasewait

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// uncheckedImageJob names a repository-owned image publisher, whose image
// repositories and tags cannot be derived from the architect job parameters.
type uncheckedImageJob struct {
	name  string
	names []string
}

func readUncheckedImageJobs(ctx context.Context, gh GitHub, owner, repo, sha string, content TagContent) ([]uncheckedImageJob, error) {
	return readConfigs(ctx, gh, owner, repo, sha, content, []string{circleCICustom}, parseUncheckedImageJobs)
}

func parseUncheckedImageJobs(config []byte) ([]uncheckedImageJob, error) {
	var doc struct {
		Jobs      yaml.Node `yaml:"jobs"`
		Workflows yaml.Node `yaml:"workflows"`
	}
	if err := yaml.Unmarshal(config, &doc); err != nil {
		return nil, fmt.Errorf("parsing the CircleCI configuration: %w", err)
	}
	runs := workflowNames(doc.Workflows)
	var jobs []uncheckedImageJob
	if doc.Jobs.Kind != yaml.MappingNode {
		return nil, nil
	}
	for i := 0; i+1 < len(doc.Jobs.Content); i += 2 {
		name := doc.Jobs.Content[i].Value
		var def struct {
			Steps []yaml.Node `yaml:"steps"`
		}
		if err := doc.Jobs.Content[i+1].Decode(&def); err != nil {
			return nil, fmt.Errorf("parsing the job %s: %w", name, err)
		}
		if !slices.ContainsFunc(imageRunCommands(def.Steps), publishesImage) {
			continue
		}
		names := runs[name]
		if len(names) == 0 {
			// custom.yml can define a job invoked by generated workflows.yml.
			names = []string{name}
		}
		jobs = append(jobs, uncheckedImageJob{name: name, names: names})
	}
	return jobs, nil
}

// imageRunCommands includes run steps nested under CircleCI when/unless.
// This inspects possible publishing commands without evaluating conditions
// or executing repository code.
func imageRunCommands(steps []yaml.Node) []string {
	commands := runCommands(steps)
	for _, step := range steps {
		if step.Kind != yaml.MappingNode {
			continue
		}
		for i := 0; i+1 < len(step.Content); i += 2 {
			if step.Content[i].Value != "when" && step.Content[i].Value != "unless" {
				continue
			}
			var nested struct {
				Steps []yaml.Node `yaml:"steps"`
			}
			if step.Content[i+1].Decode(&nested) == nil {
				commands = append(commands, imageRunCommands(nested.Steps)...)
			}
		}
	}
	return commands
}

// These are signals for a warning, not an interpretation of shell code.
// In addition to the registry CLIs, models uses its own `publish --registry`
// command. Commands hidden in scripts or other tools remain opaque.
var imagePublishCommand = regexp.MustCompile(`(?:^|[;&|()])\s*(?:(?:sudo|then|do)\s+)*(?:` +
	`(?:docker\s+push|crane\s+(?:push|copy)|skopeo\s+copy)(?:\s|$)|` +
	`docker\s+buildx\s+[^;&|]*\s--push(?:=true)?(?:\s|$)|` +
	`[^\s;&|()]+\s+publish\s+[^;&|]*--registry(?:=|\s))`)

func publishesImage(command string) bool {
	command = strings.ReplaceAll(strings.ReplaceAll(command, "\r\n", "\n"), "\\\n", " ")
	for line := range strings.SplitSeq(command, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") && imagePublishCommand.MatchString(line) {
			return true
		}
	}
	return false
}

func (w *Waiter) warnUncheckedImageJobs(jobs []uncheckedImageJob, pipelineJobs map[string]bool) {
	for _, job := range jobs {
		if slices.ContainsFunc(job.names, func(name string) bool { return pipelineJobs[name] }) {
			w.warnOnce(fmt.Sprintf("%s/%s job %s may publish images outside the architect orb: its image repositories and tags cannot be derived; images not explicitly named with --image are unchecked (--image probes each named repository at the git tag as written)", circleCIDir, circleCICustom, job.name))
		}
	}
}
