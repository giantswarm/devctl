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

// boolPtr, not the *bool literal Go lacks: schemagen.SchemaRoot.AdditionalProperties is a
// *bool (nil means "unset", vs. an explicit false).
func boolPtr(b bool) *bool { return &b }

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

	// Field-for-field the same config as schema.yaml.template; see that file for the
	// rationale behind each value (bundling, the k8sSchemaURL/k8sSchemaVersion split,
	// noAdditionalProperties, etc.).
	cfg := &schemagen.Config{
		Values: []string{
			filepath.Join(p.Dir, "helm", chartName, "zz_generated.app-platform.values.yaml"),
			filepath.Join(p.Dir, "helm", chartName, "values.yaml"),
		},
		Draft:  2020,
		Indent: 4,
		Output: tmpPath,

		Bundle:          true,
		BundleRoot:      "",
		BundleWithoutID: true,

		K8sSchemaURL:     fmt.Sprintf("https://raw.githubusercontent.com/yannh/kubernetes-json-schema/refs/heads/master/%s/", p.K8sSchemaVersion),
		K8sSchemaVersion: "v1.33.1",

		UseHelmDocs: true,

		NoAdditionalProperties: true,
		NoDefaultGlobal:        false,

		SchemaRoot: schemagen.SchemaRoot{
			AdditionalProperties: boolPtr(false),
		},
	}

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
