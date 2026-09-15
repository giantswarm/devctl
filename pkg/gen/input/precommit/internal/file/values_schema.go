package file

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/giantswarm/microerror"
	schemalintnormalize "github.com/giantswarm/schemalint/v2/pkg/normalize"
	schemagen "github.com/losisin/helm-values-schema-json/v2/pkg"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/precommit/internal/params"
)

// NewCreateValuesSchemaInput generates helm/<chart>/values.schema.json itself, in-process,
// running the same three steps the pre-commit hook used to run as a shell pipeline:
// generate (from values.yaml, via the helm-values-schema-json library) -> $ref fix
// (refFixGo) -> normalize (schemalint). devctl now writes the committed file directly; the
// hook only verifies it is up to date (see pre-commit-config.yaml.template). See
// giantswarm/devctl#2195.
func NewCreateValuesSchemaInput(p params.Params, chartName string) input.Input {
	return input.Input{
		Path: filepath.Join(p.Dir, "helm", chartName, "values.schema.json"),
		Generate: func(ctx context.Context) ([]byte, error) {
			return generateValuesSchema(ctx, p, chartName)
		},
	}
}

// k8sSchemaURLFormat is the base URL a "$ref: $k8s/..." alias in values.yaml
// expands to. %s is the Kubernetes schema version. It is a variable so the tests
// can point the fetch at an unreachable host.
var k8sSchemaURLFormat = "https://raw.githubusercontent.com/yannh/kubernetes-json-schema/refs/heads/master/%s/"

// boolPtr, not the *bool literal Go lacks: schemagen.SchemaRoot.AdditionalProperties is a
// *bool (nil means "unset", vs. an explicit false).
func boolPtr(b bool) *bool { return &b }

// newSchemaGenConfig builds the helm-values-schema-json config for chartName, writing the
// generated schema to output. It is field-for-field the same config as
// schema.yaml.template, which the read-only pre-commit hook passes to the same library via
// --config; see that file for the rationale behind each value (bundling, the
// k8sSchemaURL/k8sSchemaVersion split, noAdditionalProperties, etc.), and
// values_schema_config_test.go for the guard that keeps the two from drifting apart.
func newSchemaGenConfig(p params.Params, chartName, output string) *schemagen.Config {
	return &schemagen.Config{
		Values: []string{
			filepath.Join(p.Dir, "helm", chartName, "zz_generated.app-platform.values.yaml"),
			filepath.Join(p.Dir, "helm", chartName, "values.yaml"),
		},
		Draft:  2020,
		Indent: 4,
		Output: output,

		Bundle:          true,
		BundleRoot:      "",
		BundleWithoutID: true,

		// Both fields read the same parameter. K8sSchemaURL is already resolved here,
		// the way the rendered .schema.yaml resolves it, so the library's own
		// {{ .K8sSchemaVersion }} substitution finds nothing left to do -- but the
		// library rejects an empty K8sSchemaVersion, and a second literal here would
		// drift from the URL the moment somebody passes --k8s-schema-version.
		K8sSchemaURL:     fmt.Sprintf(k8sSchemaURLFormat, p.K8sSchemaVersion),
		K8sSchemaVersion: p.K8sSchemaVersion,

		UseHelmDocs: true,

		NoAdditionalProperties: true,
		NoDefaultGlobal:        false,

		SchemaRoot: schemagen.SchemaRoot{
			AdditionalProperties: boolPtr(false),
		},
	}
}

func generateValuesSchema(ctx context.Context, p params.Params, chartName string) ([]byte, error) {
	// helm-values-schema-json only writes its output to a file path (there is no
	// bytes-returning API), so we point it at a scratch file and read the result back
	// rather than at the real committed path -- the pipeline's later steps still have to
	// run before anything is written to helm/<chart>/values.schema.json.
	tmp, err := os.CreateTemp("", "values.schema-*.json")
	if err != nil {
		return nil, microerror.Mask(err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	cfg := newSchemaGenConfig(p, chartName, tmpPath)

	if err := schemagen.GenerateJsonSchema(ctx, cfg); err != nil {
		return nil, microerror.Mask(err)
	}

	generated, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	fixed, err := refFixGo(generated)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	normalized, err := schemalintnormalize.Normalize(fixed)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return normalized, nil
}
