package file

import (
	"path"

	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/ami/internal/params"
)

func NewAMIInput(p params.Params) input.Input {
	i := input.Input{
		Path:         path.Join(p.Dir, "aws-ami.yaml.template"),
		TemplateBody: amiTemplate,
		TemplateData: map[string]interface{}{
			"AMIInfoString": params.AMIInfoString(p),
		},
	}

	return i
}

var amiTemplate = `{{ .AMIInfoString }}`
