package workflows

import (
	"github.com/giantswarm/devctl/v6/pkg/gen"
	"github.com/giantswarm/devctl/v6/pkg/gen/input"
	"github.com/giantswarm/devctl/v6/pkg/gen/input/workflows/internal/file"
	"github.com/giantswarm/devctl/v6/pkg/gen/input/workflows/internal/params"
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

func (w *Workflows) CheckValuesSchema() input.Input {
	return file.NewCheckValuesSchemaInput(w.params)
}

func (w *Workflows) ClusterAppDocumentationValidation() input.Input {
	return file.NewClusterAppDocumentationValidation(w.params)
}

func (w *Workflows) ClusterAppSchemaValidation() input.Input {
	return file.NewClusterAppSchemaValidation(w.params)
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

func (w *Workflows) HelmRenderDiff() input.Input {
	return file.NewHelmRenderDiff(w.params)
}

func (w *Workflows) LintChangelog() input.Input {
	return file.NewLintChangelogInput(w.params)
}

func (w *Workflows) RunOSSFScorecard() input.Input {
	return file.NewRunOSSFScorecardInput(w.params)
}

func (w *Workflows) UpdateChart() input.Input {
	return file.NewUpdateChartInput(w.params)
}
