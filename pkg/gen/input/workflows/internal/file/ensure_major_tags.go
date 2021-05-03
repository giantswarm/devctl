package file

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/workflows/internal/params"
)

func NewEnsureMajorVersionTagsInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "ensure_major_version_tags.yaml"),
		TemplateBody: ensureMajorVersionTagsTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header": params.Header("#"),
		},
	}

	return i
}

var ensureMajorVersionTagsTemplate = `{{{{ .Header }}}}
name: Ensure major version tags
on:
  workflow_dispatch: {}

jobs:
  debug_info:
    name: Debug info
    runs-on: ubuntu-20.04
    steps:
      - name: Print github context JSON
        run: |
          cat <<EOF
          ${{ toJson(github) }}
          EOF
  ensure_major_version_tags:
    name: Ensure major version tags
    runs-on: ubuntu-20.04
    steps:
      - uses: actions/checkout@v2
      - uses: giantswarm/floating-tags-action@v1
`
