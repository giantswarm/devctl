package file

import (
	"path/filepath"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/ami/internal/params"
)

func NewAMIInput(p params.Params) input.Input {
	i := input.Input{
		Path:         filepath.Join(p.Dir, "aws-ami.yaml.template"),
		TemplateBody: amiTemplate,
		TemplateData: map[string]interface{}{
			"AMIInfoString": params.AMIInfoString(p),
		},
	}

	return i
}

var amiTemplate = `{{ .AMIInfoString }}`
