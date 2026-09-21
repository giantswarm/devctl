package releasewait

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/gen"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/circleci"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

// The registry layout: images under the organisation, charts under charts/
// and the organisation. Both are tagged with the bare version.
const chartsPrefix = "charts"

// imageArtifact is the reference of an image in its registry.
func imageArtifact(image, version string, private bool, endpoints agentcli.Endpoints) Artifact {
	return Artifact{
		Kind:      KindImage,
		Reference: fmt.Sprintf("%s/%s:%s", registryHost(private, endpoints), image, version),
		State:     StateMissing,
		private:   private,
	}
}

// chartArtifactFor is the reference of a chart in its registry.
func chartArtifactFor(owner, chart, catalog, version string, private bool, endpoints agentcli.Endpoints) Artifact {
	return Artifact{
		Kind:      KindChart,
		Reference: fmt.Sprintf("%s/%s/%s/%s:%s", registryHost(private, endpoints), chartsPrefix, owner, chart, version),
		State:     StateMissing,
		private:   private,
		chart:     chart,
		catalog:   catalog,
	}
}

func registryHost(private bool, endpoints agentcli.Endpoints) string {
	if private {
		return endpoints.RegistryPrivate
	}
	return endpoints.RegistryPublic
}

// GeneratedArtifacts are the artifacts devctl's CircleCI generator emits
// for a team-file entry, the way the generator derives them: an image when
// the tag has a root Dockerfile or the entry names one elsewhere, called
// gen.ci.image.name or <owner>/<repo>; a chart for the app flavour of a
// non-template repository, called gen.ci.chartName or the repository, in
// gen.ci.appCatalog or the default catalog. The private registry holds a
// private-only image and the artifacts of a private repository that does
// not force them public.
func GeneratedArtifacts(entry reposetup.Fields, repo, version string, content TagContent, privateRepo bool, endpoints agentcli.Endpoints) ([]Artifact, error) {
	if entry.Gen == nil || entry.Gen.CI == nil {
		return nil, usageErr("the team-file entry of %s has no gen.ci block to derive the artifacts from", repo)
	}
	ci := entry.Gen.CI
	owner := reposetup.DefaultOwner
	var artifacts []Artifact

	hasDockerfile := content.HasDockerfile() || (ci.Image != nil && ci.Image.Dockerfile != "")
	if hasDockerfile {
		image := owner + "/" + repo
		private := privateRepo && !ci.ForcePublic
		if ci.Image != nil {
			if ci.Image.Name != "" {
				image = ci.Image.Name
			}
			private = private || ci.Image.PrivateOnly
		}
		artifacts = append(artifacts, imageArtifact(image, version, private, endpoints))
	}

	hasApp := slices.Contains(entry.Gen.Flavours, string(gen.FlavourApp)) && entry.ComponentType != circleci.ComponentTypeTemplate
	if hasApp {
		chart := ci.ChartName
		if chart == "" {
			chart = repo
		}
		catalog := ci.AppCatalog
		if catalog == "" {
			catalog = circleci.DefaultAppCatalog
		}
		artifacts = append(artifacts, chartArtifactFor(owner, chart, catalog, version, privateRepo && !ci.ForcePublic, endpoints))
	}
	return artifacts, nil
}

// PushJob is a push job of a hand-written CircleCI configuration: an
// architect push-to-registries (or push-to-docker) job naming an image, or
// a push-to-app-catalog job naming a chart.
type PushJob struct {
	// Name is the job's name in the workflow: its name parameter, else the
	// orb job's name.
	Name string
	// Kind is image or chart.
	Kind string
	// Image is the image parameter; empty means the orb's default,
	// <owner>/<repo>.
	Image string
	// Chart and Catalog are the chart and app_catalog parameters.
	Chart, Catalog string
	// Push is false for a build-only job (push: false, or a chart job that
	// pushes to neither the catalog nor the registry).
	Push bool
	// PrivateOnly: registries-data names the private registry only.
	PrivateOnly bool
	// ForcePublic pushes a private repository's artifact to the public
	// registry.
	ForcePublic bool
}

// The orb jobs that publish, matched on their name after the orb alias.
var (
	imagePushJobs = []string{"push-to-registries", "push-to-registries-multiarch", "push-to-docker"}
	chartPushJobs = []string{"push-to-app-catalog"}
)

// ParsePushJobs reads the push jobs of every workflow in a CircleCI
// configuration. Jobs the configuration defines itself are opaque and not
// returned; the orb's push jobs are recognised whatever the orb is called.
func ParsePushJobs(config []byte) ([]PushJob, error) {
	var doc struct {
		Workflows yaml.Node `yaml:"workflows"`
	}
	if err := yaml.Unmarshal(config, &doc); err != nil {
		return nil, fmt.Errorf("parsing the CircleCI configuration: %w", err)
	}
	if doc.Workflows.Kind != yaml.MappingNode {
		return nil, nil
	}
	var images, charts []PushJob
	// The mapping node keeps the document's order, so the jobs come out as
	// the configuration lists them, images before charts.
	for i := 0; i+1 < len(doc.Workflows.Content); i += 2 {
		workflow := doc.Workflows.Content[i+1]
		if workflow.Kind != yaml.MappingNode {
			continue
		}
		var wf struct {
			Jobs []yaml.Node `yaml:"jobs"`
		}
		if err := workflow.Decode(&wf); err != nil {
			return nil, fmt.Errorf("parsing a workflow's jobs: %w", err)
		}
		for _, item := range wf.Jobs {
			if item.Kind != yaml.MappingNode || len(item.Content) < 2 {
				continue
			}
			ref := item.Content[0].Value
			orbJob := ref[strings.LastIndex(ref, "/")+1:]
			kind := ""
			switch {
			case strings.Contains(ref, "/") && slices.Contains(imagePushJobs, orbJob):
				kind = KindImage
			case strings.Contains(ref, "/") && slices.Contains(chartPushJobs, orbJob):
				kind = KindChart
			default:
				continue
			}
			var params struct {
				Name              string `yaml:"name"`
				Image             string `yaml:"image"`
				Chart             string `yaml:"chart"`
				AppCatalog        string `yaml:"app_catalog"`
				Push              *bool  `yaml:"push"`
				PushToAppCatalog  *bool  `yaml:"push_to_appcatalog"`
				PushToOCIRegistry *bool  `yaml:"push_to_oci_registry"`
				RegistriesData    string `yaml:"registries-data"`
				ForcePublic       bool   `yaml:"force-public"`
			}
			if err := item.Content[1].Decode(&params); err != nil {
				return nil, fmt.Errorf("parsing the parameters of %s: %w", ref, err)
			}
			job := PushJob{Name: params.Name, Kind: kind, Image: params.Image, Chart: params.Chart, Catalog: params.AppCatalog, Push: true, ForcePublic: params.ForcePublic}
			if job.Name == "" {
				job.Name = orbJob
			}
			switch kind {
			case KindImage:
				job.Push = params.Push == nil || *params.Push
				job.PrivateOnly = params.RegistriesData != "" && !strings.Contains(params.RegistriesData, " gsoci.azurecr.io ")
			case KindChart:
				job.Push = params.PushToOCIRegistry == nil || *params.PushToOCIRegistry
				if job.Catalog == "" {
					job.Catalog = circleci.DefaultAppCatalog
				}
			}
			if kind == KindImage {
				images = append(images, job)
			} else {
				charts = append(charts, job)
			}
		}
	}
	return append(images, charts...), nil
}

// HandWrittenArtifacts are the artifacts the tag pipeline of a hand-written
// configuration publishes: the push jobs of the configuration files at the
// tag that the pipeline runs (pipelineJobs are the job names CircleCI lists
// for it), each naming its image or chart. A Dockerfile at the tag with no
// image among them is a disagreement between the sources, reported as such.
func HandWrittenArtifacts(ctx context.Context, gh GitHub, owner, repo, sha, version string, content TagContent, pipelineJobs map[string]bool, privateRepo bool, endpoints agentcli.Endpoints) ([]Artifact, error) {
	var jobs []PushJob
	for _, name := range []string{circleCIConfig, circleCIWorkflows, circleCICustom} {
		if !slices.Contains(content.CircleCI, name) {
			continue
		}
		file, err := gh.GetFile(ctx, owner, repo, circleCIDir+"/"+name, sha)
		if err != nil {
			return nil, fmt.Errorf("reading %s/%s at %s: %w", circleCIDir, name, short(sha), err)
		}
		parsed, err := ParsePushJobs(file.Data)
		if err != nil {
			return nil, usageErr("%s/%s at %s: %v", circleCIDir, name, short(sha), err)
		}
		jobs = append(jobs, parsed...)
	}

	var artifacts []Artifact
	for _, job := range jobs {
		if !job.Push || !pipelineJobs[job.Name] {
			continue
		}
		private := privateRepo && !job.ForcePublic
		switch job.Kind {
		case KindImage:
			image := job.Image
			if image == "" {
				image = owner + "/" + repo
			}
			artifacts = append(artifacts, imageArtifact(image, version, private || job.PrivateOnly, endpoints))
		case KindChart:
			if job.Chart == "" {
				return nil, usageErr("the push job %s of the tag pipeline names no chart", job.Name)
			}
			artifacts = append(artifacts, chartArtifactFor(owner, job.Chart, job.Catalog, version, private, endpoints))
		}
	}

	hasImage := slices.ContainsFunc(artifacts, func(a Artifact) bool { return a.Kind == KindImage })
	if content.HasDockerfile() && !hasImage {
		return nil, usageErr("a Dockerfile exists at %s but no push job of the tag pipeline names an image (pipeline jobs: %s; push jobs in the configuration: %s): the sources disagree", short(sha), listOrNone(sortedKeys(pipelineJobs)), listOrNone(jobNames(jobs)))
	}
	return dedupe(artifacts), nil
}

func jobNames(jobs []PushJob) []string {
	names := make([]string, 0, len(jobs))
	for _, j := range jobs {
		names = append(names, j.Name)
	}
	return names
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// dedupe drops artifacts with a reference seen before (two jobs pushing the
// same image to the same registry).
func dedupe(artifacts []Artifact) []Artifact {
	seen := map[string]bool{}
	out := make([]Artifact, 0, len(artifacts))
	for _, a := range artifacts {
		if seen[a.Reference] {
			continue
		}
		seen[a.Reference] = true
		out = append(out, a)
	}
	return out
}
