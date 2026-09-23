package releasewait

import (
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

// fieldsFromYAML decodes one team-file entry the way the command reads it.
func fieldsFromYAML(t *testing.T, entry string) reposetup.Fields {
	t.Helper()
	tf, err := reposetup.ParseTeamFile("team-test", strings.NewReader(entry))
	if err != nil {
		t.Fatalf("parsing the entry: %v", err)
	}
	if len(tf.Entries) != 1 {
		t.Fatalf("expected one entry, got %d", len(tf.Entries))
	}
	fields, err := tf.Entries[0].Fields()
	if err != nil {
		t.Fatalf("decoding the entry: %v", err)
	}
	return fields
}

var testEndpoints = agentcli.Endpoints{RegistryPublic: "public.example", RegistryPrivate: "private.example"}

func TestGeneratedArtifacts(t *testing.T) {
	cases := []struct {
		name    string
		entry   string
		root    []string
		private bool
		// charts are the Chart.yaml names of the helm/ directories at the
		// tag; a directory not listed has none.
		charts     map[string]string
		want       []string
		wantErr    string
		privateRef []string
	}{
		{
			name: "renamed image and a chart",
			entry: `- name: kserve
  gen:
    flavours: [app]
    language: go
    ci:
      generate: true
      image:
        name: giantswarm/kserve-controller
`,
			root:   []string{"Dockerfile", "helm"},
			charts: map[string]string{"kserve": "kserve"},
			want:   []string{"image public.example/giantswarm/kserve-controller:1.2.3", "chart public.example/charts/giantswarm/kserve:1.2.3"},
		},
		{
			name: "cli repository with a Dockerfile: image only, named after the repository",
			entry: `- name: devctl
  gen:
    flavours: [cli]
    language: go
    ci:
      generate: true
`,
			root: []string{"Dockerfile", "main.go"},
			want: []string{"image public.example/giantswarm/devctl:1.2.3"},
		},
		{
			name: "chart-only repository with a chart name override",
			entry: `- name: docs-proxy
  gen:
    flavours: [app]
    language: generic
    ci:
      generate: true
      chartName: docs-proxy-app
      appCatalog: giantswarm-operations-platform
`,
			root:   []string{"helm", "README.md"},
			charts: map[string]string{"docs-proxy-app": "docs-proxy-app"},
			want:   []string{"chart public.example/charts/giantswarm/docs-proxy-app:1.2.3"},
		},
		{
			name: "a Dockerfile elsewhere turns the image on",
			entry: `- name: backstage
  gen:
    flavours: [app]
    language: node
    ci:
      generate: true
      image:
        dockerfile: packages/backend/Dockerfile
`,
			root:   []string{"helm", "packages"},
			charts: map[string]string{"backstage": "backstage"},
			want:   []string{"image public.example/giantswarm/backstage:1.2.3", "chart public.example/charts/giantswarm/backstage:1.2.3"},
		},
		{
			name: "private-only image of a public repository",
			entry: `- name: secret-operator
  gen:
    flavours: [app]
    language: go
    ci:
      generate: true
      image:
        privateOnly: true
`,
			root:       []string{"Dockerfile", "helm"},
			charts:     map[string]string{"secret-operator": "secret-operator"},
			want:       []string{"image private.example/giantswarm/secret-operator:1.2.3", "chart public.example/charts/giantswarm/secret-operator:1.2.3"},
			privateRef: []string{"private.example/giantswarm/secret-operator:1.2.3"},
		},
		{
			name: "private repository: everything private",
			entry: `- name: internal-thing
  gen:
    flavours: [app]
    language: go
    ci:
      generate: true
`,
			root:       []string{"Dockerfile", "helm"},
			private:    true,
			charts:     map[string]string{"internal-thing": "internal-thing"},
			want:       []string{"image private.example/giantswarm/internal-thing:1.2.3", "chart private.example/charts/giantswarm/internal-thing:1.2.3"},
			privateRef: []string{"private.example/giantswarm/internal-thing:1.2.3", "private.example/charts/giantswarm/internal-thing:1.2.3"},
		},
		{
			name: "private repository forced public",
			entry: `- name: web-assets
  gen:
    flavours: [app]
    language: generic
    ci:
      generate: true
      forcePublic: true
`,
			root:    []string{"Dockerfile", "helm"},
			private: true,
			charts:  map[string]string{"web-assets": "web-assets"},
			want:    []string{"image public.example/giantswarm/web-assets:1.2.3", "chart public.example/charts/giantswarm/web-assets:1.2.3"},
		},
		{
			name: "template repository has no chart",
			entry: `- name: template-app
  componentType: template
  gen:
    flavours: [app]
    language: generic
    ci:
      generate: true
`,
			root: []string{"helm"},
			want: nil,
		},
		{
			name: "release assets only: no Dockerfile, no app flavour",
			entry: `- name: agentlab
  gen:
    flavours: [cli]
    language: go
    ci:
      generate: true
`,
			root: []string{"main.go"},
			want: nil,
		},
		{
			// An entry without gen.ci declares no override: the generator's
			// defaults name the artifacts (a pipeline rendered from the entry
			// before its gen.ci block was declared has exactly those).
			name: "no gen.ci block: the generator's defaults",
			entry: `- name: plain
  gen:
    flavours: [app]
    language: go
`,
			root:   []string{"Dockerfile", "helm"},
			charts: map[string]string{"plain": "plain"},
			want:   []string{"image public.example/giantswarm/plain:1.2.3", "chart public.example/charts/giantswarm/plain:1.2.3"},
		},
		{
			// The chart directory predates the repository's rename: the
			// pipeline packages helm/widget and publishes the chart under the
			// name its Chart.yaml declares.
			name: "chart directory and published name differ",
			entry: `- name: widget-app
  gen:
    flavours: [app]
    language: go
    ci:
      generate: true
      chartName: widget
      image:
        name: giantswarm/widget
`,
			root:       []string{"Dockerfile", "helm"},
			private:    true,
			charts:     map[string]string{"widget": "widget-app"},
			want:       []string{"image private.example/giantswarm/widget:1.2.3", "chart private.example/charts/giantswarm/widget-app:1.2.3"},
			privateRef: []string{"private.example/giantswarm/widget:1.2.3", "private.example/charts/giantswarm/widget-app:1.2.3"},
		},
		{
			name: "the packaged directory has no Chart.yaml",
			entry: `- name: widget-app
  gen:
    flavours: [app]
    language: go
    ci:
      generate: true
`,
			root:    []string{"helm"},
			charts:  map[string]string{"widget": "widget-app"},
			wantErr: "helm/widget-app has no Chart.yaml",
		},
		{
			name: "no gen block",
			entry: `- name: plain
`,
			root:    []string{"Dockerfile"},
			wantErr: "no gen block",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fields := fieldsFromYAML(t, tc.entry)
			content := TagContent{SHA: "abc", Root: tc.root}
			got, err := GeneratedArtifacts(fields, fields.Name, "1.2.3", content, tc.private, testEndpoints, fakeChartNames(tc.charts))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertArtifacts(t, got, tc.want, tc.privateRef)
		})
	}
}

// fakeChartNames names the charts of the helm/ directories in names; any
// other directory has no Chart.yaml.
func fakeChartNames(names map[string]string) ChartNames {
	return func(dir string) (string, error) {
		name, ok := names[dir]
		if !ok {
			return "", usageErr("helm/%s has no Chart.yaml", dir)
		}
		return name, nil
	}
}

func assertArtifacts(t *testing.T, got []Artifact, want []string, privateRefs []string) {
	t.Helper()
	var refs []string
	for _, a := range got {
		refs = append(refs, a.Kind+" "+a.Reference)
		if a.State != StateMissing || a.Digest != "" {
			t.Errorf("%s: expected missing without a digest, got %s %q", a.Reference, a.State, a.Digest)
		}
		wantPrivate := false
		for _, p := range privateRefs {
			if p == a.Reference {
				wantPrivate = true
			}
		}
		if a.Private() != wantPrivate {
			t.Errorf("%s: private %v, want %v", a.Reference, a.Private(), wantPrivate)
		}
	}
	if strings.Join(refs, "\n") != strings.Join(want, "\n") {
		t.Errorf("artifacts:\n want %v\n got  %v", want, refs)
	}
}

const handWrittenConfig = `version: 2.1
orbs:
  architect: giantswarm/architect@6.0.0
workflows:
  build:
    jobs:
      - architect/go-build:
          name: go-build
      - architect/push-to-registries:
          context: architect
          name: build-image
          push: false
          image: giantswarm/kserve-controller
          requires: [go-build]
      - architect/push-to-registries:
          context: architect
          name: push-to-registries-release
          image: giantswarm/kserve-controller
          filters:
            tags:
              only: /^v.*/
      - architect/push-to-registries:
          context: architect
          name: push-llmisvc
          image: giantswarm/llmisvc-controller
          registries-data: |-
            private gsociprivate.azurecr.io ACR_GSOCIPRIVATE_USERNAME ACR_GSOCIPRIVATE_PASSWORD true
      - architect/push-to-app-catalog:
          name: push-chart
          chart: kserve
          app_catalog: giantswarm-catalog
          requires: [push-to-registries-release]
      - architect/push-to-app-catalog:
          name: build-chart
          chart: kserve
          push_to_appcatalog: false
          push_to_oci_registry: false
  nightly:
    triggers:
      - schedule:
          cron: "0 3 * * *"
    jobs:
      - architect/push-to-registries:
          name: push-nightly
          image: giantswarm/kserve-controller
`

func TestParsePushJobs(t *testing.T) {
	jobs, err := ParsePushJobs([]byte(handWrittenConfig))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]PushJob{
		"build-image":                {Name: "build-image", Kind: KindImage, Image: "giantswarm/kserve-controller", Push: false},
		"push-to-registries-release": {Name: "push-to-registries-release", Kind: KindImage, Image: "giantswarm/kserve-controller", Push: true},
		"push-llmisvc":               {Name: "push-llmisvc", Kind: KindImage, Image: "giantswarm/llmisvc-controller", Push: true, PrivateOnly: true},
		"push-chart":                 {Name: "push-chart", Kind: KindChart, Chart: "kserve", Catalog: "giantswarm-catalog", Push: true},
		"build-chart":                {Name: "build-chart", Kind: KindChart, Chart: "kserve", Catalog: "giantswarm-catalog", Push: false},
		"push-nightly":               {Name: "push-nightly", Kind: KindImage, Image: "giantswarm/kserve-controller", Push: true},
	}
	if len(jobs) != len(want) {
		t.Fatalf("want %d push jobs, got %d: %+v", len(want), len(jobs), jobs)
	}
	for _, job := range jobs {
		if w, ok := want[job.Name]; !ok || w != job {
			t.Errorf("job %s: want %+v, got %+v", job.Name, w, job)
		}
	}
}

func TestParsePushJobsUnnamedAndOtherOrbAlias(t *testing.T) {
	config := `version: 2
workflows:
  release:
    jobs:
      - gs/push-to-docker:
          image: giantswarm/thing
      - build
      - gs/push-to-app-catalog:
          chart: thing-app
`
	jobs, err := ParsePushJobs([]byte(config))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("want 2 push jobs, got %+v", jobs)
	}
	if jobs[0].Name != "push-to-docker" || jobs[0].Image != "giantswarm/thing" || !jobs[0].Push {
		t.Errorf("image job: %+v", jobs[0])
	}
	if jobs[1].Name != "push-to-app-catalog" || jobs[1].Chart != "thing-app" || jobs[1].Catalog != "giantswarm-catalog" {
		t.Errorf("chart job: %+v", jobs[1])
	}
}
