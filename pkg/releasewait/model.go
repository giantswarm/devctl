package releasewait

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

// The files the models are read from.
const (
	dockerfile        = "Dockerfile"
	workflowsDir      = ".github/workflows"
	circleCIDir       = ".circleci"
	circleCIConfig    = "config.yml"
	circleCIWorkflows = "workflows.yml"
	circleCICustom    = "custom.yml"
)

// TagContent is what the tag carries of the files the models and the
// artifacts are read from: the root listing, the workflow files and the
// CircleCI directory.
type TagContent struct {
	// SHA the content was read at.
	SHA string
	// Root are the names at the repository root.
	Root []string
	// Workflows are the file names under .github/workflows.
	Workflows []string
	// CircleCI are the file names under .circleci.
	CircleCI []string
}

// ReadTagContent lists the three directories at sha; a missing directory is
// an empty list.
func ReadTagContent(ctx context.Context, gh GitHub, owner, repo, sha string) (*TagContent, error) {
	list := func(path string) ([]string, error) {
		names, err := gh.ListDirectory(ctx, owner, repo, path, sha)
		if githubclient.IsNotFound(err) {
			return []string{}, nil
		}
		if err != nil {
			return nil, fmt.Errorf("listing %q at %s: %w", path, short(sha), err)
		}
		return names, nil
	}
	var c TagContent
	var err error
	c.SHA = sha
	if c.Root, err = list(""); err != nil {
		return nil, err
	}
	if c.Workflows, err = list(workflowsDir); err != nil {
		return nil, err
	}
	if c.CircleCI, err = list(circleCIDir); err != nil {
		return nil, err
	}
	return &c, nil
}

// HasDockerfile: a Dockerfile at the repository root, the signal that turns
// the generated image pipeline on and the reason a hand-written pipeline
// without a push job is a disagreement.
func (c TagContent) HasDockerfile() bool { return slices.Contains(c.Root, dockerfile) }

// HasCircleCI: the repository has a CircleCI project to read.
func (c TagContent) HasCircleCI() bool { return slices.Contains(c.CircleCI, circleCIConfig) }

// GeneratedCircleCI: the configuration is devctl's, recognised by the
// workflows.yml the generator writes beside config.yml.
func (c TagContent) GeneratedCircleCI() bool {
	return c.HasCircleCI() && slices.Contains(c.CircleCI, circleCIWorkflows)
}

// HasCustomCircleCI: a repo-owned custom.yml is merged into the generated
// workflows.
func (c TagContent) HasCustomCircleCI() bool { return slices.Contains(c.CircleCI, circleCICustom) }

// CIModel is the CI model the files show.
func (c TagContent) CIModel() string {
	switch {
	case !c.HasCircleCI():
		return CIModelNone
	case c.GeneratedCircleCI():
		return CIModelGenerated
	}
	return CIModelHandWritten
}

// ReleaseModel is the release model the workflow files show: auto-release
// for an auto-release workflow, legacy for the create-release workflows,
// "" when neither is there. Both at once is an error: the files contradict
// each other.
func (c TagContent) ReleaseModel() (string, error) {
	var auto, legacy []string
	for _, name := range c.Workflows {
		n := strings.ToLower(name)
		switch {
		case strings.Contains(n, "auto_release") || strings.Contains(n, "auto-release"):
			auto = append(auto, name)
		case strings.Contains(n, "create_release") || strings.Contains(n, "create-release"):
			legacy = append(legacy, name)
		}
	}
	switch {
	case len(auto) > 0 && len(legacy) > 0:
		return "", usageErr("the workflows at %s carry both the auto-release workflow (%s) and the legacy release workflows (%s)", short(c.SHA), strings.Join(auto, ", "), strings.Join(legacy, ", "))
	case len(auto) > 0:
		return ReleaseModelAutoRelease, nil
	case len(legacy) > 0:
		return ReleaseModelLegacy, nil
	}
	return "", nil
}

// Models are the settled release and CI models.
type Models struct {
	Release string
	CI      string
}

// EntryReleaseModel is the release model a team-file entry declares:
// releaseWorkflow when set, else auto-release with gen.ci.generate and
// legacy without. An entry without a gen block declares nothing ("").
func EntryReleaseModel(entry *reposetup.Fields) string {
	if entry == nil || entry.Gen == nil {
		return ""
	}
	ci := entry.Gen.CI
	if ci != nil && ci.ReleaseWorkflow != "" {
		return ci.ReleaseWorkflow
	}
	if EntryGeneratesCI(entry) {
		return ReleaseModelAutoRelease
	}
	return ReleaseModelLegacy
}

// EntryGeneratesCI: the entry opts into devctl-generated CircleCI
// configuration.
func EntryGeneratesCI(entry *reposetup.Fields) bool {
	return entry != nil && entry.Gen != nil && entry.Gen.CI != nil && entry.Gen.CI.Generate != nil && *entry.Gen.CI.Generate
}

// ResolveModels settles the models from the team-file entry (nil when no
// team file declares the repository) cross-checked against the tag's files.
// A disagreement between the two is an error, never a guess; a source that
// says nothing leaves the decision to the other; neither saying anything
// about the release model is an error too.
func ResolveModels(entry *reposetup.Fields, content TagContent) (Models, error) {
	fromFiles, err := content.ReleaseModel()
	if err != nil {
		return Models{}, err
	}
	fromEntry := EntryReleaseModel(entry)
	var m Models
	switch {
	case fromEntry != "" && fromFiles != "" && fromEntry != fromFiles:
		return Models{}, usageErr("the team-file entry says the release workflow is %s but the workflows at %s are the %s ones (%s)", fromEntry, short(content.SHA), fromFiles, strings.Join(content.Workflows, ", "))
	case fromEntry != "":
		m.Release = fromEntry
	case fromFiles != "":
		m.Release = fromFiles
	default:
		return Models{}, usageErr("cannot tell how the repository releases: no team-file entry with a gen block and no release workflow under %s at %s", workflowsDir, short(content.SHA))
	}

	m.CI = content.CIModel()
	explicit := entry != nil && entry.Gen != nil && entry.Gen.CI != nil && entry.Gen.CI.Generate != nil
	switch {
	case EntryGeneratesCI(entry) && m.CI != CIModelGenerated:
		return Models{}, usageErr("the team-file entry has gen.ci.generate true but %s at %s has no generated %s (found: %s)", circleCIDir, short(content.SHA), circleCIWorkflows, listOrNone(content.CircleCI))
	case explicit && !EntryGeneratesCI(entry) && m.CI == CIModelGenerated:
		return Models{}, usageErr("the team-file entry has gen.ci.generate false but %s at %s carries the generated %s", circleCIDir, short(content.SHA), circleCIWorkflows)
	}
	return m, nil
}

func listOrNone(names []string) string {
	if len(names) == 0 {
		return "nothing"
	}
	return strings.Join(names, ", ")
}
