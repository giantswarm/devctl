package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/circleci/internal/params"
)

//go:embed setup-config.yml.template
var setupConfigTemplate string

//go:embed workflows.yml.template
var workflowsTemplate string

// NewSetupConfigInput emits .circleci/config.yml: a static dynamic-config
// setup workflow that merges the optional repo-owned .circleci/custom.yml
// into the generated .circleci/workflows.yml at pipeline runtime and
// continues with the result. It carries no repo-specific content -- only the
// continuation orb pin varies, and only with devctl releases.
func NewSetupConfigInput(p params.Params) input.Input {
	i := input.Input{
		Path:         ".circleci/config.yml",
		TemplateBody: setupConfigTemplate,
		TemplateData: map[string]interface{}{
			"ContinuationOrbVersion": p.ContinuationOrbVersion,
		},
	}

	return i
}

// NewWorkflowsInput emits .circleci/workflows.yml: the golden pipeline
// content, derived from the repo's signals. The setup workflow continues the
// pipeline with this file (plus the optional custom.yml merge).
func NewWorkflowsInput(p params.Params) input.Input {
	i := input.Input{
		Path:         ".circleci/workflows.yml",
		TemplateBody: workflowsTemplate,
		TemplateData: map[string]interface{}{
			"RepoName":         p.RepoName,
			"Language":         p.Language,
			"HasDockerfile":    p.HasDockerfile,
			"HasApp":           p.HasApp,
			"ChartName":        p.ChartName,
			"ForcePublic":      p.ForcePublic,
			"AppCatalog":       p.AppCatalog,
			"AppCatalogTest":   p.AppCatalogTest,
			"BranchPublish":    p.BranchPublish,
			"ImagePreBuildJob": p.ImagePreBuildJob,
			"ImagePrivateOnly": p.ImagePrivateOnly,
			"ImageName":        p.ImageName,
			"ImagePlatforms":   p.ImagePlatforms,
			"ReleaseBinaries":  p.ReleaseBinaries,
			"OrbVersion":       p.OrbVersion,
		},
	}

	return i
}
