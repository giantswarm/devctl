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
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

// The registry layout: images under the organisation, charts under charts/
// and the organisation. Both are tagged with the bare version.
const chartsPrefix = "charts"

// A chart's sources: the directory under helm/ the pipeline packages and the
// Chart.yaml in it that names what is published.
const (
	helmDir   = "helm"
	chartFile = "Chart.yaml"
)

// ChartNames names the chart in a directory under helm/: the name it is
// published under.
type ChartNames func(dir string) (string, error)

// TagChartNames reads the names from helm/<dir>/Chart.yaml at sha. The
// architect orb packages the directory its chart parameter names and helm
// push names the OCI repository after the packaged chart, so the published
// name is the Chart.yaml's; the directory of a repository renamed after its
// chart was created still carries the old name. A Chart.yaml that is missing,
// unreadable as YAML or without a name is an error naming the file, never
// the directory in its place.
func TagChartNames(ctx context.Context, gh GitHub, owner, repo, sha string) ChartNames {
	return func(dir string) (string, error) {
		path := helmDir + "/" + dir + "/" + chartFile
		file, err := gh.GetFile(ctx, owner, repo, path, sha)
		if githubclient.IsNotFound(err) {
			return "", usageErr("%s does not exist at %s: the pipeline packages %s/%s and publishes the chart under the name that file declares", path, short(sha), helmDir, dir)
		}
		if err != nil {
			return "", fmt.Errorf("reading %s at %s: %w", path, short(sha), err)
		}
		var chart struct {
			Name string `yaml:"name"`
		}
		if err := yaml.Unmarshal(file.Data, &chart); err != nil {
			return "", usageErr("parsing %s at %s: %v", path, short(sha), err)
		}
		if chart.Name == "" {
			return "", usageErr("%s at %s declares no name, the name the chart is published under", path, short(sha))
		}
		return chart.Name, nil
	}
}

// imageArtifact is the reference of an image in its registry.
func imageArtifact(image, version string, private bool, endpoints agentcli.Endpoints) Artifact {
	return Artifact{
		Kind:      KindImage,
		Reference: fmt.Sprintf("%s/%s:%s", registryHost(private, endpoints), image, version),
		State:     StateMissing,
		private:   private,
	}
}

// chartArtifactFor is the reference of a chart in its registry, with the
// production and the test catalog its pipeline pushes it to.
func chartArtifactFor(owner, chart, catalog, catalogTest, version string, private bool, endpoints agentcli.Endpoints) Artifact {
	return Artifact{
		Kind:        KindChart,
		Reference:   fmt.Sprintf("%s/%s/%s/%s:%s", registryHost(private, endpoints), chartsPrefix, owner, chart, version),
		State:       StateMissing,
		private:     private,
		chart:       chart,
		catalog:     catalog,
		catalogTest: catalogTest,
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
// non-template repository, packaged from helm/<gen.ci.chartName> or
// helm/<repo> and called what charts names that directory, in
// gen.ci.appCatalog or the default catalog (gen.ci.appCatalogTest or the
// default test catalog for a pre-release). The private registry holds a
// private-only image and the artifacts of a private repository that does
// not force them public.
func GeneratedArtifacts(entry reposetup.Fields, repo, version string, content TagContent, privateRepo bool, endpoints agentcli.Endpoints, charts ChartNames) ([]Artifact, error) {
	if entry.Gen == nil {
		return nil, usageErr("the team-file entry of %s has no gen block to derive the artifacts from", repo)
	}
	// An entry without gen.ci declares no override: the generator's defaults
	// name the artifacts, as they do for a pipeline rendered from the entry
	// before its gen.ci block was declared.
	ci := entry.Gen.CI
	if ci == nil {
		ci = &reposetup.CIFields{}
	}
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
		dir := ci.ChartName
		if dir == "" {
			dir = repo
		}
		chart, err := charts(dir)
		if err != nil {
			return nil, err
		}
		catalog := ci.AppCatalog
		if catalog == "" {
			catalog = circleci.DefaultAppCatalog
		}
		catalogTest := ci.AppCatalogTest
		if catalogTest == "" {
			catalogTest = circleci.DefaultAppCatalogTest
		}
		artifacts = append(artifacts, chartArtifactFor(owner, chart, catalog, catalogTest, version, privateRepo && !ci.ForcePublic, endpoints))
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
	// Chart, Catalog and CatalogTest are the chart, app_catalog and
	// app_catalog_test parameters; Chart is the directory under helm/ the job
	// packages.
	Chart, Catalog, CatalogTest string
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
				AppCatalogTest    string `yaml:"app_catalog_test"`
				Push              *bool  `yaml:"push"`
				PushToAppCatalog  *bool  `yaml:"push_to_appcatalog"`
				PushToOCIRegistry *bool  `yaml:"push_to_oci_registry"`
				RegistriesData    string `yaml:"registries-data"`
				ForcePublic       bool   `yaml:"force-public"`
			}
			if err := item.Content[1].Decode(&params); err != nil {
				return nil, fmt.Errorf("parsing the parameters of %s: %w", ref, err)
			}
			job := PushJob{Name: params.Name, Kind: kind, Image: params.Image, Chart: params.Chart, Catalog: params.AppCatalog, CatalogTest: params.AppCatalogTest, Push: true, ForcePublic: params.ForcePublic}
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
				if job.CatalogTest == "" {
					job.CatalogTest = circleci.DefaultAppCatalogTest
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
// for it), each naming its image or the chart directory whose Chart.yaml
// names the chart, and the images the caller names (--image) for a job that
// pushes outside the architect orb, tagged with the git tag as written: such
// a job pushes $CIRCLE_TAG, where the orb strips the v, and the charts the
// caller names (--chart) for a job that pushes them with helm itself,
// tagged with the bare version as helm requires. A Dockerfile at the tag with no image
// among them is a usage error naming --image; the repository name is never
// taken for the image.
func HandWrittenArtifacts(ctx context.Context, gh GitHub, owner, repo, sha, version, tag string, content TagContent, pipelineJobs map[string]bool, privateRepo bool, images, charts []string, endpoints agentcli.Endpoints) ([]Artifact, error) {
	jobs, artifacts, err := pushJobArtifacts(ctx, gh, owner, repo, sha, version, content, []string{circleCIConfig, circleCIWorkflows, circleCICustom}, pipelineJobs, privateRepo, endpoints)
	if err != nil {
		return nil, err
	}

	for _, name := range images {
		path, private, err := namedImage(name, endpoints)
		if err != nil {
			return nil, err
		}
		if private == nil {
			private = &privateRepo
		}
		artifacts = append(artifacts, imageArtifact(path, tag, *private, endpoints))
	}
	for _, name := range charts {
		path, private, err := namedChart(name, endpoints)
		if err != nil {
			return nil, err
		}
		if private == nil {
			private = &privateRepo
		}
		artifacts = append(artifacts, namedChartArtifact(path, version, *private, endpoints))
	}

	hasImage := slices.ContainsFunc(artifacts, func(a Artifact) bool { return a.Kind == KindImage })
	if content.HasDockerfile() && !hasImage {
		return nil, usageErr("a Dockerfile exists at %s but no push job of the tag pipeline names an image (pipeline jobs: %s; architect push jobs in the configuration: %s): a job that pushes outside the architect orb (a plain docker push) cannot be read from the configuration, and the repository name is never taken for the image; name it with devctl release wait --image, e.g. --image %s/%s", short(sha), listOrNone(sortedKeys(pipelineJobs)), listOrNone(jobNames(jobs)), owner, repo)
	}
	return dedupe(artifacts), nil
}

// CustomPushJobs are the architect push jobs the repository's own custom.yml
// adds to a generated pipeline, read at the tag: what the pipeline publishes
// beyond the team-file entry's artifacts (a second chart released off the
// same tag, giantswarm/agent-platform's connectivity chart). None for a
// custom.yml of test and repository-owned jobs alone, which then names no
// artifact and needs no look at the pipeline's jobs.
func CustomPushJobs(ctx context.Context, gh GitHub, owner, repo, sha string, content TagContent) ([]PushJob, error) {
	jobs, err := readPushJobs(ctx, gh, owner, repo, sha, content, []string{circleCICustom})
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(jobs, func(job PushJob) bool { return !job.Push }), nil
}

// CustomArtifacts are the artifacts of the custom push jobs (CustomPushJobs)
// that the tag pipeline runs, pipelineJobs being the job names CircleCI
// lists for it.
func CustomArtifacts(ctx context.Context, gh GitHub, owner, repo, sha, version string, jobs []PushJob, pipelineJobs map[string]bool, privateRepo bool, endpoints agentcli.Endpoints) ([]Artifact, error) {
	return pushJobArtifactsOf(ctx, gh, owner, repo, sha, version, jobs, pipelineJobs, privateRepo, endpoints)
}

// pushJobArtifacts reads the push jobs of the named configuration files at
// the tag and returns them with the artifacts of those the tag pipeline runs.
func pushJobArtifacts(ctx context.Context, gh GitHub, owner, repo, sha, version string, content TagContent, files []string, pipelineJobs map[string]bool, privateRepo bool, endpoints agentcli.Endpoints) ([]PushJob, []Artifact, error) {
	jobs, err := readPushJobs(ctx, gh, owner, repo, sha, content, files)
	if err != nil {
		return nil, nil, err
	}
	artifacts, err := pushJobArtifactsOf(ctx, gh, owner, repo, sha, version, jobs, pipelineJobs, privateRepo, endpoints)
	if err != nil {
		return nil, nil, err
	}
	return jobs, artifacts, nil
}

// readPushJobs reads the push jobs of the named configuration files at the
// tag; a file the tag does not carry is skipped.
func readPushJobs(ctx context.Context, gh GitHub, owner, repo, sha string, content TagContent, files []string) ([]PushJob, error) {
	var jobs []PushJob
	for _, name := range files {
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
	return jobs, nil
}

// pushJobArtifactsOf are the artifacts of the push jobs among jobs that the
// tag pipeline runs.
func pushJobArtifactsOf(ctx context.Context, gh GitHub, owner, repo, sha, version string, jobs []PushJob, pipelineJobs map[string]bool, privateRepo bool, endpoints agentcli.Endpoints) ([]Artifact, error) {
	charts := TagChartNames(ctx, gh, owner, repo, sha)
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
			chart, err := charts(job.Chart)
			if err != nil {
				return nil, err
			}
			artifacts = append(artifacts, chartArtifactFor(owner, chart, job.Catalog, job.CatalogTest, version, private, endpoints))
		}
	}
	return artifacts, nil
}

// namedImage reads an image the caller names: a repository path such as
// giantswarm/dex, or one with the public or private registry host in front,
// which then decides the registry (private nil: the repository's
// visibility does). A tag, a digest or another registry is a usage error.
func namedImage(name string, endpoints agentcli.Endpoints) (path string, private *bool, err error) {
	return namedPath("--image", "image", "giantswarm/dex", name, endpoints)
}

// namedChart reads a chart the caller names the way namedImage reads an
// image: giantswarm/kagent/helm/kagent, optionally after a registry host.
func namedChart(name string, endpoints agentcli.Endpoints) (path string, private *bool, err error) {
	return namedPath("--chart", "chart", "giantswarm/kagent/helm/kagent", name, endpoints)
}

// namedChartArtifact is a chart --chart names, at its own repository path:
// the chart is its last element, and it goes to no catalog.
func namedChartArtifact(path, version string, private bool, endpoints agentcli.Endpoints) Artifact {
	return Artifact{
		Kind:      KindChart,
		Reference: fmt.Sprintf("%s/%s:%s", registryHost(private, endpoints), path, version),
		State:     StateMissing,
		private:   private,
		chart:     path[strings.LastIndex(path, "/")+1:],
	}
}

func namedPath(flag, kind, example, name string, endpoints agentcli.Endpoints) (path string, private *bool, err error) {
	path = name
	for host, isPrivate := range map[string]bool{endpoints.RegistryPublic: false, endpoints.RegistryPrivate: true} {
		if rest, ok := strings.CutPrefix(name, host+"/"); ok {
			path, private = rest, &isPrivate
		}
	}
	first, _, _ := strings.Cut(path, "/")
	switch {
	case !strings.Contains(path, "/") || strings.ContainsAny(first, ".:"):
		return "", nil, usageErr("%s %q: name the %s as <owner>/<name> (e.g. %s), optionally after %s or %s", flag, name, kind, example, endpoints.RegistryPublic, endpoints.RegistryPrivate)
	case strings.ContainsAny(path, ":@"):
		return "", nil, usageErr("%s %q: name the %s without a tag or digest; the release's version is the tag", flag, name, kind)
	}
	return path, private, nil
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
