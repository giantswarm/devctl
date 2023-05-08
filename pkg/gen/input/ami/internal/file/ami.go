package file

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/ami/internal/params"
)

func NewAMIInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "ami.go"),
		TemplateBody: amiTemplate,
		TemplateData: map[string]interface{}{
			"Header":        params.Header("//"),
			"AMIInfoString": params.AMIInfoString(p),
			"Package":       params.Package(p),
		},
	}

	return i
}

var amiTemplate = `{{ .Header }}

package {{ .Package }}

import "encoding/json"

var amiInfo = map[string]map[string]string{}

var amiJSON = []byte({{ .AMIInfoString }})

func init() {
	err := json.Unmarshal(amiJSON, &amiInfo)
	if err != nil {
		panic(err)
	}
}
`
