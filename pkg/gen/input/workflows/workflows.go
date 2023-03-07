package workflows

import (
	"github.com/giantswarm/devctl/pkg/gen"
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/workflows/internal/file"
	"github.com/giantswarm/devctl/pkg/gen/input/workflows/internal/params"
)

type Config struct {
	EnableFloatingMajorVersionTags bool
	Flavours                       gen.FlavourSlice
}

type Workflows struct {
	params params.Params
}

func New(config Config) (*Workflows, error) {
	w := &Workflows{
		params: params.Params{
			Dir: ".github/workflows",

			EnableFloatingMajorVersionTags: config.EnableFloatingMajorVersionTags,
			Flavours:                       config.Flavours,
		},
	}

	return w, nil
}

func (w *Workflows) AddCustomerBoardAutomation() input.Input {
	return file.NewCustomerBoardAutomationInput(w.params)
}

func (w *Workflows) CreateRelease() input.Input {
	return file.NewCreateReleaseInput(w.params)
}

func (w *Workflows) CreateReleasePR() input.Input {
	return file.NewCreateReleasePRInput(w.params)
}

func (w *Workflows) EnsureMajorVersionTags() input.Input {
	return file.NewEnsureMajorVersionTagsInput(w.params)
}

func (w *Workflows) Gitleaks() input.Input {
	return file.NewGitleaksInput(w.params)
}

func (w *Workflows) CheckValuesSchema() input.Input {
	return file.NewCheckValuesSchemaInput(w.params)
}

func (w *Workflows) UpdateChart() input.Input {
	return file.NewUpdateChartInput(w.params)
}

func (w *Workflows) JSONSchemaValidation() input.Input {
	return file.NewJSONSchemaValidation(w.params)
}
