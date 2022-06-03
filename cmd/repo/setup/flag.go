package setup

import (
	"github.com/spf13/cobra"
)

type flag struct {
	GithubTokenEnvVar string

	// Features
	EnableWiki     bool
	EnableIssues   bool
	EnableProjects bool
	Archived       bool

	// Merge settings
	AllowMergeCommit bool
	AllowSquashMerge bool
	AllowRebaseMerge bool

	// Pull requests
	AllowUpdateBranch   bool
	AllowAutoMerge      bool
	DeleteBranchOnMerge bool

	// Permissions
	Permissions map[string]string

	// Branch protection
	Checks []string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVar(&f.GithubTokenEnvVar, "github-token-envvar", "GITHUB_TOKEN", "Environement variable name for Github token.")

	// Features
	cmd.PersistentFlags().BoolVar(&f.EnableWiki, "enable-wiki", false, "Either true to enable the wiki for this repository or false to disable it.")
	cmd.PersistentFlags().BoolVar(&f.EnableIssues, "enable-issues", true, "Either true to enable issues for this repository or false to disable them.")
	cmd.PersistentFlags().BoolVar(&f.EnableProjects, "enable-projects", false, "Either true to enable projects for this repository or false to disable them.")
	cmd.PersistentFlags().BoolVar(&f.Archived, "archived", false, "true to archive this repository.")

	// Merge settings
	cmd.PersistentFlags().BoolVar(&f.AllowMergeCommit, "allow-mergecommit", false, "Either true to allow merging pull requests with a merge commit, or false to prevent merging pull requests with merge commits.")
	cmd.PersistentFlags().BoolVar(&f.AllowSquashMerge, "allow-squashmerge", true, "Either true to allow squash-merging pull requests, or false to prevent squash-merging.")
	cmd.PersistentFlags().BoolVar(&f.AllowRebaseMerge, "allow-rebasemerge", false, "Either true to allow rebase-merging pull requests, or false to prevent rebase-merging.")

	// Pull requests
	cmd.PersistentFlags().BoolVar(&f.AllowUpdateBranch, "allow-updatebranch", true, "Whenever there are new changes available in the base branch, present an “update branch” option in the pull request.")
	cmd.PersistentFlags().BoolVar(&f.AllowAutoMerge, "allow-automerge", true, "Either true to allow auto-merge on pull requests, or false to disallow auto-merge.")
	cmd.PersistentFlags().BoolVar(&f.DeleteBranchOnMerge, "delete-branch-on-merge", true, "Either true to allow automatically deleting head branches when pull requests are merged, or false to prevent automatic deletion.")

	// Permissions
	cmd.PersistentFlags().StringToStringVar(&f.Permissions, "permissions", map[string]string{"Employees": "admin", "bots": "push"}, "Grant access to this repository using github_team_name=permission format. Multiple values can be provided as a comma separated list or using this flag multiple times. Permission can be one of: pull, push, admin, maintain, triage.")

	// Branch protection
	cmd.PersistentFlags().StringSliceVar(&f.Checks, "checks", nil, "Check context names for branch protection. Default will add all auto-detected checks, this can be disabled by passing an empty string.")
}

func (f *flag) Validate() error {
	return nil
}
