package apptest

import (
	"github.com/spf13/cobra"
)

type flag struct {
	AppName  string
	RepoName string
	Catalog  string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.AppName, "app-name", "", "The name of the app in the catalog")
	cmd.Flags().StringVar(&f.RepoName, "repo-name", "", "The name of the repo")
	cmd.Flags().StringVar(&f.Catalog, "catalog", "", "The name of the catalog the app belongs to")
}

func (f *flag) Validate() error {
	return nil
}
