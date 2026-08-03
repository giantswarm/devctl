package file

import (
	_ "embed"
	"path/filepath"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/precommit/internal/params"
)

//go:generate go run ../../../update-template-sha.go pre-commit-config.yaml.template
//go:embed pre-commit-config.yaml.template
var createPreCommitConfigTemplate string

//go:embed pre-commit-config.yaml.template.sha
var createPreCommitConfigTemplateSha string

// refFixPython is the middle step of the generated helm-schema pipeline hook: for every
// object that has both `$ref` and `additionalProperties: false` it drops
// `additionalProperties` and sets `unevaluatedProperties: false` instead. Only the latter
// keyword considers properties pulled in through `$ref` in JSON Schema 2020-12
// (losisin/helm-values-schema-json#317); see the template for the full rationale.
//
// It lives here rather than inline in the template so Test_RefFixPython can execute the
// exact program that gets generated. Two constraints:
//   - no single quote: the template embeds this in a YAML single-quoted scalar, itself
//     wrapped in shell single quotes.
//   - explicit `encoding="utf-8"`: `open()` would otherwise decode with the ambient locale,
//     which is ASCII under `LC_ALL=C`, and helm-docs descriptions routinely carry
//     non-ASCII characters that `helm schema` writes out as raw UTF-8.
//
// No indentation or trailing newline is written: `schemalint normalize` runs right after
// and is the last writer, so any formatting done here would be discarded.
const refFixPython = `import json,sys; h=lambda o: {**{k:v for k,v in o.items() if k!="additionalProperties"},"unevaluatedProperties":False} if ("$ref" in o and o.get("additionalProperties") is False) else o; p=sys.argv[1]; f=open(p,encoding="utf-8"); d=json.load(f,object_hook=h); f.close(); f=open(p,"w",encoding="utf-8"); json.dump(d,f); f.close()`

func NewCreatePreCommitConfigInput(p params.Params) input.Input {
	i := input.Input{
		Path:         filepath.Join(p.Dir, ".pre-commit-config.yaml"),
		TemplateBody: createPreCommitConfigTemplate,
		TemplateData: map[string]interface{}{
			"Header":          params.Header("#", createPreCommitConfigTemplateSha),
			"Language":        p.Language,
			"HasBash":         params.HasFlavor(p, "bash"),
			"HasMd":           params.HasFlavor(p, "md"),
			"HasHelmchart":    params.HasFlavor(p, "helmchart"),
			"RepoName":        p.RepoName,
			"HelmCharts":      p.HelmCharts,
			"RefFixPython":    refFixPython,
			"NodeRunPrefix":   p.NodeRunPrefix,
			"NodeDevLintHook": p.NodeDevLintHook,
		},
	}

	return i
}
