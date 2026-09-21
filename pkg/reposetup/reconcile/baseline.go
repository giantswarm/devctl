package reconcile

// Baseline is the repository set-up every declared repository gets on top
// of its declaration: the settings, team permissions and branch protection
// of giantswarm/giantswarm#36733 as `devctl repo setup` applies them, the
// required-check rule, the webhooks, and where the catalog and the mapping
// live. [DefaultBaseline] is the company's; a caller adjusts a copy.
type Baseline struct {
	// DefaultBranch is the protected default branch.
	DefaultBranch string `json:"defaultBranch"`

	// Features.
	HasWiki     bool `json:"hasWiki"`
	HasIssues   bool `json:"hasIssues"`
	HasProjects bool `json:"hasProjects"`

	// Merge settings.
	AllowMergeCommit bool `json:"allowMergeCommit"`
	AllowSquashMerge bool `json:"allowSquashMerge"`
	AllowRebaseMerge bool `json:"allowRebaseMerge"`

	// Pull requests.
	AllowUpdateBranch   bool `json:"allowUpdateBranch"`
	AllowAutoMerge      bool `json:"allowAutoMerge"`
	DeleteBranchOnMerge bool `json:"deleteBranchOnMerge"`

	// WorkflowPermissions is the default GITHUB_TOKEN permission of the
	// repository's workflows: read or write.
	WorkflowPermissions string `json:"workflowPermissions"`

	// TeamPermissions maps a team slug to the permission it holds on every
	// repository: pull, triage, push, maintain or admin.
	TeamPermissions map[string]string `json:"teamPermissions"`

	// Branch protection: classic without a devctl App id, the default
	// branch's ruleset ([RulesetName]) with one.
	RequiredReviews int `json:"requiredReviews"`
	// EnforceAdmins binds administrators to classic protection too. True in
	// [DefaultBaseline], the company baseline: what `devctl repo setup`
	// applies and what the merge tool lifts and restores around a merge.
	// The ruleset has no such switch: everyone but its bypass actors is
	// bound.
	EnforceAdmins bool `json:"enforceAdmins"`
	// StrictChecks requires a branch to be up to date before it merges.
	// False in [DefaultBaseline]: on a repository with Renovate and sweep
	// traffic every merge would invalidate every other open pull request
	// and re-run its CI.
	StrictChecks bool `json:"strictChecks"`
	// RequiredChecks are required whatever reported.
	RequiredChecks []string `json:"requiredChecks,omitempty"`
	// RequiredChecksIfReported are required once they have reported on the
	// default branch or a recently merged pull request — the generated
	// GitHub Actions gates. The generated CircleCI pipeline's jobs are
	// candidates the same way, read from the repository.
	RequiredChecksIfReported []string `json:"requiredChecksIfReported,omitempty"`
	// IgnoredChecks are regular expressions of contexts never required and
	// removed when found required: release workflows, path-filtered
	// workflows, the dependency-graph submission — contexts that report
	// on the default branch but cannot report on every pull request.
	IgnoredChecks []string `json:"ignoredChecks,omitempty"`

	// Webhooks every repository carries; none by default (CircleCI installs
	// its own on follow).
	Webhooks []Webhook `json:"webhooks,omitempty"`

	// RenovateInstallationID is the Renovate GitHub App installation whose
	// repository list the renovate step reads as detail when the token can
	// (an organization owner's; a GitHub App token cannot); 0 reads none.
	// The step's verdict comes from the repository's own evidence.
	RenovateInstallationID int64 `json:"renovateInstallationID"`

	// CatalogRepository holds the catalog and the two workflows, as
	// owner/name.
	CatalogRepository string `json:"catalogRepository"`
	// CatalogPath is the catalog file listing the components.
	CatalogPath string `json:"catalogPath"`
	// CatalogWorkflow regenerates the catalog (input force).
	CatalogWorkflow string `json:"catalogWorkflow"`
	// MappingRepository holds the apps-to-teams mapping, as owner/name.
	MappingRepository string `json:"mappingRepository"`
	// MappingPath is the mapping ConfigMap file.
	MappingPath string `json:"mappingPath"`
	// MappingWorkflow regenerates the mapping (input repository), in
	// CatalogRepository.
	MappingWorkflow string `json:"mappingWorkflow"`
}

// Webhook is one webhook of the baseline.
type Webhook struct {
	URL         string   `json:"url"`
	Events      []string `json:"events"`
	ContentType string   `json:"contentType"`
	// Secret is the shared secret; it is written, never read back.
	Secret string `json:"-"`
}

// renovateInstallationID is the Renovate App installation of the giantswarm
// organization, https://github.com/organizations/giantswarm/settings/installations/17164699.
const renovateInstallationID = 17164699

// DefaultBaseline is the company baseline: what `devctl repo setup` applies
// by default, the required-check rule of `devctl repo checks`, the catalog
// and mapping of giantswarm/github and management-cluster-bases.
func DefaultBaseline() Baseline {
	return Baseline{
		DefaultBranch:       "main",
		HasWiki:             false,
		HasIssues:           true,
		HasProjects:         false,
		AllowMergeCommit:    false,
		AllowSquashMerge:    true,
		AllowRebaseMerge:    false,
		AllowUpdateBranch:   true,
		AllowAutoMerge:      true,
		DeleteBranchOnMerge: true,
		WorkflowPermissions: "write",
		TeamPermissions:     map[string]string{"employees": "admin", "bots": "push"},
		RequiredReviews:     1,
		EnforceAdmins:       true,
		StrictChecks:        false,
		RequiredChecksIfReported: []string{
			"semantic-pull-request / Validate PR title",
			"pre-commit",
		},
		IgnoredChecks: []string{
			`^create-release`,
			`^update-go_modules-graph$`,
			`aliyun`,
			`^validate-changelog$`,
			`^check-values-schema$`,
		},
		RenovateInstallationID: renovateInstallationID,
		CatalogRepository:      "giantswarm/github",
		CatalogPath:            "catalog/components.yaml",
		CatalogWorkflow:        "update-devportal-catalog.yaml",
		MappingRepository:      "giantswarm/management-cluster-bases",
		MappingPath:            "bases/apps-to-teams-mapping/configmap.yaml",
		MappingWorkflow:        "apps-to-teams-mapping.yaml",
	}
}
