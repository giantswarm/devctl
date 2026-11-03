package file

import (
	"embed"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

//go:embed reservation_command.yaml.template
var reservationCommandTemplate string

//go:generate go run ../../../update-template-sha.go reservation_command.yaml.template
//go:embed reservation_command.yaml.template*
var reservationCommandTemplateFiles embed.FS

var reservationCommandTemplateSha = input.TemplateSHA(reservationCommandTemplateFiles, "reservation_command.yaml.template")

func NewReservationCommandInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "reservation_command.yaml"),
		TemplateBody: reservationCommandTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  templateDelimLeft,
			Right: templateDelimRight,
		},
		TemplateData: map[string]interface{}{
			templateKeyHeader: params.Header("#", reservationCommandTemplateSha),
		},
	}

	return i
}
