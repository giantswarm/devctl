package precommit_test

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giantswarm/micrologger"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"github.com/giantswarm/devctl/v8/cmd/gen/precommit"
)

func TestHelmGeneration_Output(t *testing.T) {
	t.Chdir(t.TempDir())
	writeHelmFixture(t, "first", "replicaCount: 1\n")
	// A unique synthetic reference also avoids reading a user's schema cache
	// on platforms where XDG_CACHE_HOME does not select os.UserCacheDir.
	refFile := "fixture-" + filepath.Base(t.TempDir()) + ".json"
	writeHelmFixture(t, "second", "resources: {} # @schema $ref: $k8s/"+refFile+"#/definitions/Fixture\n")
	const customReadme = "custom {{ template \"chart.header\" . }}\n"
	require.NoError(t, os.WriteFile("helm/first/README.md.gotmpl", []byte(customReadme), 0o600))
	// Regeneration must refresh this file before the schema reads it.
	require.NoError(t, os.WriteFile("helm/first/zz_generated.app-platform.values.yaml", []byte("stale: true\n"), 0o600))

	const version = "v1.29.0"
	wantURL := "https://raw.githubusercontent.com/yannh/kubernetes-json-schema/refs/heads/master/" + version + "/" + refFile
	var requests []string
	installHelmTransport(t, func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req.URL.String())
		if req.Method != http.MethodGet || req.URL.String() != wantURL {
			return nil, fmt.Errorf("unexpected schema request: %s %s", req.Method, req.URL)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"definitions":{"Fixture":{
				"type":"object","properties":{"fixtureValue":{"type":"string"}},
				"additionalProperties":false}}}`)),
		}, nil
	})

	require.NoError(t, executeHelmCommand(t, "--language=generic", "--flavors=helmchart", "--k8s-schema-version="+version))
	require.Equal(t, []string{wantURL}, requests)
	for _, chart := range []string{"first", "second"} {
		var config struct {
			K8sSchemaVersion string   `json:"k8sSchemaVersion"`
			K8sSchemaURL     string   `json:"k8sSchemaURL"`
			Values           []string `json:"values"`
		}
		require.NoError(t, yaml.Unmarshal(readHelmFile(t, "helm/"+chart+"/.schema.yaml"), &config))
		require.Equal(t, version, config.K8sSchemaVersion)
		require.Equal(t, strings.TrimSuffix(wantURL, refFile), config.K8sSchemaURL)
		require.Equal(t, []string{"helm/" + chart + "/zz_generated.app-platform.values.yaml", "helm/" + chart + "/values.yaml"}, config.Values)
	}
	first := compileHelmSchema(t, "first")
	require.NoError(t, first.Validate(map[string]any{"replicaCount": 1, "managementCluster": "fixture"}))
	require.Error(t, first.Validate(map[string]any{"stale": true}), "the schema must read the refreshed platform values")
	second := compileHelmSchema(t, "second")
	require.NoError(t, second.Validate(map[string]any{"resources": map[string]any{"fixtureValue": "fixture"}}))
	require.Error(t, second.Validate(map[string]any{"resources": map[string]any{"fixtureValue": 1}}), "the bundled schema must enforce the fetched type")
	require.Equal(t, customReadme, string(readHelmFile(t, "helm/first/README.md.gotmpl")))
	require.Contains(t, string(readHelmFile(t, "helm/second/README.md.gotmpl")), `{{ template "chart.header" . }}`)
	config := string(readHelmFile(t, ".pre-commit-config.yaml"))
	for _, chart := range []string{"first", "second"} {
		require.Contains(t, config, "id: helm-schema-"+chart)
	}
	require.FileExists(t, ".github/workflows/zz_generated.pre-commit.yaml")
}

func TestHelmGeneration_WithoutFlavor(t *testing.T) {
	t.Chdir(t.TempDir())
	writeHelmFixture(t, "fixture", "invalid: [\n")
	paths := []string{"values.schema.json", ".schema.yaml", "zz_generated.app-platform.values.yaml", "README.md.gotmpl"}
	for _, path := range paths {
		require.NoError(t, os.WriteFile("helm/fixture/"+path, []byte("existing "+path+"\n"), 0o600))
	}
	installHelmTransport(t, func(req *http.Request) (*http.Response, error) {
		t.Errorf("unexpected schema request without helmchart flavor: %s", req.URL)
		return nil, fmt.Errorf("unexpected schema request")
	})

	require.NoError(t, executeHelmCommand(t, "--language=generic"))
	for _, path := range paths {
		require.Equal(t, "existing "+path+"\n", string(readHelmFile(t, "helm/fixture/"+path)))
	}
	require.NotContains(t, string(readHelmFile(t, ".pre-commit-config.yaml")), "id: helm-schema-")
}

func TestHelmGeneration_LaterChartFailure(t *testing.T) {
	for _, failure := range []string{"schema request", "malformed values", "destination directory"} {
		t.Run(failure, func(t *testing.T) {
			t.Chdir(t.TempDir())
			writeHelmFixture(t, "a-first", "firstOnly: fixture\n")
			values := "replicaCount: 1\n"
			refFile := "fixture-" + filepath.Base(t.TempDir()) + ".json"
			switch failure {
			case "schema request":
				values = "resources: {} # @schema $ref: $k8s/" + refFile + "#/definitions/Fixture\n"
			case "malformed values":
				values = "invalid: [\n"
			}
			writeHelmFixture(t, "b-failed", values)
			writeHelmFixture(t, "c-later", "laterOnly: fixture\n")
			const original = "{\"committed\":\"schema\"}\n"
			failedPath := "helm/b-failed/values.schema.json"
			if failure == "destination directory" {
				require.NoError(t, os.Mkdir(failedPath, 0o750))
				failedPath += "/sentinel"
			}
			for _, path := range []string{"helm/a-first/values.schema.json", failedPath, "helm/c-later/values.schema.json"} {
				require.NoError(t, os.WriteFile(path, []byte(original), 0o600))
			}
			var requests []string
			installHelmTransport(t, func(req *http.Request) (*http.Response, error) {
				requests = append(requests, req.URL.String())
				return &http.Response{
					StatusCode: http.StatusServiceUnavailable,
					Status:     "503 Service Unavailable",
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("synthetic schema failure")),
				}, nil
			})

			err := executeHelmCommand(t, "--language=generic", "--flavors=helmchart", "--k8s-schema-version=v1.29.0")
			require.Error(t, err)
			switch failure {
			case "schema request":
				wantURL := "https://raw.githubusercontent.com/yannh/kubernetes-json-schema/refs/heads/master/v1.29.0/" + refFile
				require.ErrorContains(t, err, wantURL)
				require.ErrorContains(t, err, "503 Service Unavailable")
				require.Equal(t, []string{wantURL}, requests)
			case "malformed values":
				require.ErrorContains(t, err, "error unmarshalling YAML")
			case "destination directory":
				require.ErrorContains(t, err, "values.schema.json")
				require.ErrorContains(t, err, "directory")
			}
			require.Equal(t, original, string(readHelmFile(t, failedPath)), "the failed destination must survive")
			require.Equal(t, original, string(readHelmFile(t, "helm/c-later/values.schema.json")), "later schemas must not be generated")
			first := compileHelmSchema(t, "a-first")
			require.NoError(t, first.Validate(map[string]any{"firstOnly": "fixture"}), "earlier schema generation already completed")
			for _, chart := range []string{"a-first", "b-failed", "c-later"} {
				require.NoFileExists(t, "helm/"+chart+"/README.md.gotmpl", "README scaffolding must not run after the error")
			}
		})
	}
}

func writeHelmFixture(t *testing.T, name, values string) {
	t.Helper()
	dir := filepath.Join("helm", name)
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Chart.yaml"), []byte("name: "+name+"\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "values.yaml"), []byte(values), 0o600))
}

func executeHelmCommand(t *testing.T, args ...string) error {
	t.Helper()
	logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
	require.NoError(t, err)
	command, err := precommit.New(precommit.Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
	require.NoError(t, err)
	command.SilenceErrors = true
	command.SilenceUsage = true
	command.SetArgs(args)
	return command.ExecuteContext(t.Context())
}

func readHelmFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- test fixture paths under t.TempDir only
	require.NoError(t, err)
	return data
}

func compileHelmSchema(t *testing.T, chart string) *jsonschema.Schema {
	t.Helper()
	compiler := jsonschema.NewCompiler() // Its default loader only reads local files.
	schema, err := compiler.Compile(filepath.Join("helm", chart, "values.schema.json"))
	require.NoError(t, err)
	return schema
}

type helmRoundTripper func(*http.Request) (*http.Response, error)

func (f helmRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func installHelmTransport(t *testing.T, transport helmRoundTripper) {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("LocalAppData", t.TempDir())
	previous := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: transport}
	t.Cleanup(func() { http.DefaultClient = previous })
}
