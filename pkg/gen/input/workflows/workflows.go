package workflows

import (
	"github.com/giantswarm/devctl/v8/pkg/gen"
	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/file"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

type Config struct {
	Flavours gen.FlavourSlice
}

type Workflows struct {
	params params.Params
}

func New(config Config) (*Workflows, error) {
	w := &Workflows{
		params: params.Params{
			Dir:      ".github/workflows",
			Flavours: config.Flavours,
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
func (w *Workflows) ClusterAppValuesValidationUsingSchema() input.Input {
	return file.NewClusterAppValuesValidationUsingSchemaTemplate(w.params)
}

func (w *Workflows) ClusterAppSchemaValidation() input.Input {
	return file.NewClusterAppSchemaValidation(w.params)
}

func (w *Workflows) AutoRelease() input.Input {
	return file.NewAutoReleaseInput(w.params)
}

func (w *Workflows) AutoReleaseDeletion() input.Input {
	return file.NewAutoReleaseDeletionInput(w.params)
}

func (w *Workflows) AutoReleaseLegacyDeletion() input.Input {
	return file.NewAutoReleaseLegacyDeletionInput(w.params)
}

func (w *Workflows) CliffToml() input.Input {
	return file.NewCliffTomlInput()
}

func (w *Workflows) CliffTomlDeletion() input.Input {
	return file.NewCliffTomlDeletionInput()
}

func (w *Workflows) CreateRelease() input.Input {
	return file.NewCreateReleaseInput(w.params)
}

func (w *Workflows) CreateReleaseDeletion() input.Input {
	return file.NewCreateReleaseDeletionInput(w.params)
}

func (w *Workflows) CreateReleasePR() input.Input {
	return file.NewCreateReleasePRInput(w.params)
}

func (w *Workflows) CreateReleasePRDeletion() input.Input {
	return file.NewCreateReleasePRDeletionInput(w.params)
}

func (w *Workflows) DispatchUpdateChartEvents(targetRepo string) input.Input {
	return file.NewDispatchUpdateChartEventsInput(w.params, targetRepo)
}

func (w *Workflows) FixVulnerabilities() input.Input {
	return file.NewFixVulnerabilitiesInput(w.params)
}

func (w *Workflows) Gitleaks() input.Input {
	return file.NewGitleaksInput(w.params)
}

func (w *Workflows) HelmRenderDiff() input.Input {
	return file.NewHelmRenderDiff(w.params)
}

func (w *Workflows) PublishTechdocsInput() input.Input {
	return file.NewPublishTechdocs(w.params)
}

func (w *Workflows) RunOSSFScorecard() input.Input {
	return file.NewRunOSSFScorecardInput(w.params)
}

func (w *Workflows) SemanticPullRequest() input.Input {
	return file.NewSemanticPullRequestInput(w.params)
}

func (w *Workflows) SyncFromUpstream() input.Input {
	return file.NewSyncFromUpstreamInput(w.params)
}

func (w *Workflows) TestKyvernoPoliciesWithChainsaw() input.Input {
	return file.NewTestKyvernoPoliciesWithChainsawInput(w.params)
}

func (w *Workflows) UpdateChart() input.Input {
	return file.NewUpdateChartInput(w.params)
}

func (w *Workflows) ValidateChangelog() input.Input {
	return file.NewValidateChangelogInput(w.params)
}

func (w *Workflows) ValidateChangelogDeletion() input.Input {
	return file.NewValidateChangelogDeletionInput(w.params)
}

func (w *Workflows) AnalyzeGithubActions() input.Input {
	return file.NewAnalyzeGithubActionsInput(w.params)
}

func (w *Workflows) ZizmorBaseYml() input.Input {
	return file.NewZizmorBaseInput(w.params)
}
