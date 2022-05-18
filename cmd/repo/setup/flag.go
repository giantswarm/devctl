package setup

import (
	"github.com/spf13/cobra"
)

type flag struct {
	HasWiki             bool
	HasIssues           bool
	HasProjects         bool
	Archived            bool
	AllowMergeCommit    bool
	AllowSquashMerge    bool
	AllowRebaseMerge    bool
	AllowUpdateBranch   bool
	AllowAutoMerge      bool
	DeleteBranchOnMerge bool
	Permissions         map[string]string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.PersistentFlags().BoolVar(&f.HasWiki, "has-wiki", false, "Either true to enable the wiki for this repository or false to disable it.")
	cmd.PersistentFlags().BoolVar(&f.HasIssues, "has-issues", true, "Either true to enable issues for this repository or false to disable them.")
	cmd.PersistentFlags().BoolVar(&f.HasProjects, "has-projects", false, "Either true to enable projects for this repository or false to disable them.")
	cmd.PersistentFlags().BoolVar(&f.Archived, "archived", false, "true to archive this repository.")
	cmd.PersistentFlags().BoolVar(&f.AllowMergeCommit, "allow-mergecommit", false, "Either true to allow merging pull requests with a merge commit, or false to prevent merging pull requests with merge commits.")
	cmd.PersistentFlags().BoolVar(&f.AllowSquashMerge, "allow-squashmerge", true, "Either true to allow squash-merging pull requests, or false to prevent squash-merging.")
	cmd.PersistentFlags().BoolVar(&f.AllowRebaseMerge, "allow-rebasemerge", false, "Either true to allow rebase-merging pull requests, or false to prevent rebase-merging.")
	cmd.PersistentFlags().BoolVar(&f.AllowUpdateBranch, "allow-updatebranch", true, "Whenever there are new changes available in the base branch, present an “update branch” option in the pull request.")
	cmd.PersistentFlags().BoolVar(&f.AllowAutoMerge, "allow-automerge", true, "Either true to allow auto-merge on pull requests, or false to disallow auto-merge.")
	cmd.PersistentFlags().BoolVar(&f.DeleteBranchOnMerge, "delete-branch-on-merge", true, "Either true to allow automatically deleting head branches when pull requests are merged, or false to prevent automatic deletion..")
	cmd.PersistentFlags().StringToStringVar(&f.Permissions, "permission", map[string]string{"Employees": "admin", "bots": "push"}, "map of Github Team slug and permissions. Permission can be one of: pull, push, admin, maintain, triage")
}

func (f *flag) Validate() error {
	return nil
}
