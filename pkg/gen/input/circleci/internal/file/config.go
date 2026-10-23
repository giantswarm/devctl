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

//go:embed multi-image-setup-config.yml.template
var multiImageSetupConfigTemplate string

//go:embed multi-image-workflows.yml.template
var multiImageWorkflowsTemplate string

// releaseBranches matches the maintenance branches the generated auto-release
// workflow tags (release-2.x, release-v3.7.x, ...), as a CircleCI branch
// filter. Every branch-only job ignores them like main: a merge there is tagged
// within seconds, so the commit's version is the release, which the tag
// pipeline alone builds and publishes (architect refuses a branch build that
// resolves a release version).
const releaseBranches = `/^release-v?[0-9]+(\.[0-9]+)?\.x$/`

// NewSetupConfigInput emits .circleci/config.yml: a static dynamic-config
// setup workflow that merges the optional repo-owned .circleci/custom.yml
// into the generated .circleci/workflows.yml at pipeline runtime and
// continues with the result. It carries no repo-specific content -- only the
// continuation orb pin varies, and only with devctl releases.
func NewSetupConfigInput(p params.Params) input.Input {
	if p.MultiImage {
		return newMultiImageSetupConfigInput(p)
	}

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
	if p.MultiImage {
		return newMultiImageWorkflowsInput(p)
	}

	i := input.Input{
		Path:         ".circleci/workflows.yml",
		TemplateBody: workflowsTemplate,
		TemplateData: map[string]interface{}{
			"RepoName":            p.RepoName,
			"Language":            p.Language,
			"HasDockerfile":       p.HasDockerfile,
			"HasApp":              p.HasApp,
			"SkipAppCatalog":      p.SkipAppCatalog,
			"SkipATS":             p.SkipATS,
			"ATSVersion":          p.ATSVersion,
			"ATSKindCluster":      p.ATSKindCluster,
			"ATSKindNodeImage":    p.ATSKindNodeImage,
			"ATSImageRegistry":    p.ATSImageRegistry,
			"ATSPullImages":       p.ATSPullImages,
			"ATSKindConfig":       p.ATSKindConfig,
			"ATSResourceClass":    p.ATSResourceClass,
			"ATSOnRelease":        p.ATSOnRelease,
			"ChartName":           p.ChartName,
			"ChartNameMismatch":   p.ChartNameMismatch,
			"KeepChartAppVersion": p.KeepChartAppVersion,
			"ForcePublic":         p.ForcePublic,
			"AppCatalog":          p.AppCatalog,
			"AppCatalogTest":      p.AppCatalogTest,
			"BranchPublish":       p.BranchPublish,
			"ReleaseBranches":     releaseBranches,
			"ImagePreBuildJob":    p.ImagePreBuildJob,
			"ChartReleaseGateJob": p.ChartReleaseGateJob,
			"ImagePrivateOnly":    p.ImagePrivateOnly,
			"ImageName":           p.ImageName,
			"OwnImages":           p.OwnImages,
			"ImagePlatforms":      p.ImagePlatforms,
			"ImageNativeBuilds":   p.ImageNativeBuilds,
			"BranchImageBuilds":   p.BranchImageBuilds,
			"ReleaseImageBuilds":  p.ReleaseImageBuilds,
			"ImageDockerfile":     p.ImageDockerfile,
			"ReleaseBinaries":     p.ReleaseBinaries,
			"BuildConcurrency":    p.BuildConcurrency,
			"ResourceClass":       p.ResourceClass,
			"GoBuildPath":         p.GoBuildPath,
			"GoTestArtifacts":     p.GoTestArtifacts,
			"OrbVersion":          p.OrbVersion,

			"BuildJobName":        p.BuildJobName,
			"NodeJobName":         p.NodeJobName,
			"NodeImageVersion":    p.NodeImageVersion,
			"NodeInstallCommand":  p.NodeInstallCommand,
			"NodeRunPrefix":       p.NodeRunPrefix,
			"NodeCachePath":       p.NodeCachePath,
			"NodeCacheKey":        p.NodeCacheKey,
			"NodeCacheRestoreKey": p.NodeCacheRestoreKey,
			"NodeBuildCachePaths": p.NodeBuildCachePaths,
			"NodeBuildCacheKey":   p.NodeBuildCacheKey,
			"NodeCorepack":        p.NodeCorepack,
			"NodeResourceClass":   p.NodeResourceClass,
			"NodeJestMaxWorkers":  p.NodeJestMaxWorkers,
			"NodeLintConcurrency": p.NodeLintConcurrency,
			"NodeTestTarget":      p.NodeTestTarget,
			"NodeBuildTarget":     p.NodeBuildTarget,
			"NodeBuildOutput":     p.NodeBuildOutput,

			"TemplateChart": p.TemplateChart,
			"Team":          p.Team,
			// The template contract, handed to the template so the placeholders
			// and fixtures are spelled in one place (the circleci package).
			"TemplateAppNamePlaceholder":        p.TemplateAppNamePlaceholder,
			"TemplateTeamPlaceholder":           p.TemplateTeamPlaceholder,
			"TemplateHelmRepositoryPlaceholder": p.TemplateHelmRepositoryPlaceholder,
			"TemplateAppName":                   p.TemplateAppName,
			"TemplateHelmRepository":            p.TemplateHelmRepository,
		},
	}

	return i
}

// newMultiImageSetupConfigInput emits the .circleci/config.yml of a
// multi-image repository: a setup workflow that picks the images to build at
// pipeline runtime (the tag's image, or the images a branch changed) and
// continues with one copy of the per-image workflow of each, plus the
// optional custom.yml merge.
func newMultiImageSetupConfigInput(p params.Params) input.Input {
	return input.Input{
		Path:         ".circleci/config.yml",
		TemplateBody: multiImageSetupConfigTemplate,
		TemplateData: map[string]interface{}{
			"ContinuationOrbVersion": p.ContinuationOrbVersion,
			"DefaultBranch":          p.DefaultBranch,
			"ImagesDir":              p.ImagesDir,
		},
	}
}

// newMultiImageWorkflowsInput emits the .circleci/workflows.yml of a
// multi-image repository: the per-image branch and release workflows, with
// the placeholder __IMAGE__ the setup workflow replaces with each image's
// name.
func newMultiImageWorkflowsInput(p params.Params) input.Input {
	return input.Input{
		Path:         ".circleci/workflows.yml",
		TemplateBody: multiImageWorkflowsTemplate,
		TemplateData: map[string]interface{}{
			"DefaultBranch":   p.DefaultBranch,
			"ForcePublic":     p.ForcePublic,
			"ImageNamePrefix": p.ImageNamePrefix,
			"ImagePlatforms":  p.ImagePlatforms,
			"ImagesDir":       p.ImagesDir,
			"OrbVersion":      p.OrbVersion,
			"Registry":        p.Registry,
		},
	}
}
