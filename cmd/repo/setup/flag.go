package setup

import (
	"github.com/spf13/cobra"
)

type flag struct {
	DryRun            bool
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

	// Branch
	DefaultBranch           string
	DisableBranchProtection bool
	Checks                  []string
	ChecksFilter            string
}

func (f *flag) Init(cmd *cobra.Command) {
	// Persistent flags are also available to subcommands.
	cmd.PersistentFlags().StringVar(&f.GithubTokenEnvVar, "github-token-envvar", "GITHUB_TOKEN", "Environment variable name for Github token.")

	// Standard flags
	cmd.Flags().BoolVar(&f.DryRun, "dry-run", false, "Dry-run or ready-only mode. Show what is being made but do not apply any change.")

	// Features
	cmd.Flags().BoolVar(&f.EnableWiki, "enable-wiki", false, "Enable wiki for this repo, false to remove it.")
	cmd.Flags().BoolVar(&f.EnableIssues, "enable-issues", true, "Enable issues for this repo, or false to remove them.")
	cmd.Flags().BoolVar(&f.EnableProjects, "enable-projects", false, "Enable projects for this repo, or false to remove them.")
	cmd.Flags().BoolVar(&f.Archived, "archived", false, "Mark this repo as archived.")

	// Merge settings
	cmd.Flags().BoolVar(&f.AllowMergeCommit, "allow-mergecommit", false, "Allow merging pull requests with a merge commit, or false to prevent it.")
	cmd.Flags().BoolVar(&f.AllowSquashMerge, "allow-squashmerge", true, "Allow squash-merging pull requests, or false to prevent it.")
	cmd.Flags().BoolVar(&f.AllowRebaseMerge, "allow-rebasemerge", false, "Allow rebase-merging pull requests, or false to prevent it.")

	// Pull requests
	cmd.Flags().BoolVar(&f.AllowUpdateBranch, "allow-updatebranch", true, "Whenever there are new changes available in the base branch, present an “update branch” option in the pull request.")
	cmd.Flags().BoolVar(&f.AllowAutoMerge, "allow-automerge", true, "Allow auto-merge on pull requests, or false to forbid it.")
	cmd.Flags().BoolVar(&f.DeleteBranchOnMerge, "delete-branch-on-merge", true, "Automatically delete head branches when PRs are merged, or false to prevent it.")

	// Permissions
	cmd.Flags().StringToStringVar(&f.Permissions, "permissions", map[string]string{"Employees": "admin", "bots": "push"}, "Grant access to this repository using github_team_name=permission format. Multiple values can be provided as a comma separated list or using this flag multiple times. Permission can be one of: pull, push, admin, maintain, triage.")

	// Branch
	cmd.Flags().StringVar(&f.DefaultBranch, "default-branch", "main", "Default branch name")
	cmd.Flags().BoolVar(&f.DisableBranchProtection, "disable-branch-protection", false, "Disable default branch protection")
	cmd.Flags().StringSliceVar(&f.Checks, "checks", nil, "Check context names for branch protection. Default will add all auto-detected checks, this can be disabled by passing an empty string. Overrides \"--checks-filter\"")
	cmd.Flags().StringVar(&f.ChecksFilter, "checks-filter", "aliyun", "Provide a regex to filter checks. Checks matching the regex will be ignored. Empty string disables filter (all checks are accepted).")
}

func (f *flag) Validate() error {
	return nil
}
