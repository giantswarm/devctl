package release

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/github"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
	formatConfig "gopkg.in/src-d/go-git.v4/plumbing/format/config"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
)

type flag struct {
	Author                *object.Signature
	Client                *github.Client
	Organization          string
	RepositoryName        string
	Release               string
	Repository            string
	RepositoryPath        string
	TagName               string
	Token                 string
	WorkInProgressVersion string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.Release, "release", "", `Release number that you want to publish.`)
	cmd.Flags().StringVar(&f.Repository, "repository", "", `Repository of the code that you want to release.`)
	cmd.Flags().StringVar(&f.RepositoryPath, "repositoryPath", "", `Path where the git repository lives in your file system.`)
}

func (f *flag) Validate() error {
	var err error
	if f.Release == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", "release")
	}
	f.TagName = fmt.Sprintf("v%s", f.Release)
	f.WorkInProgressVersion = fmt.Sprintf("%s-dev", f.Release)

	if f.Repository == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty. Format is owner/repo, like giantswarm/azure-operator.", "repository")
	}

	repoParts := strings.Split(f.Repository, "/")
	if len(repoParts) != 2 {
		return microerror.Maskf(invalidRepoError, "repository format is owner/repo, like giantswarm/azure-operator")
	}
	f.Organization = repoParts[0]
	f.RepositoryName = repoParts[1]

	f.Author, err = getAuthorFromGitConfigFile()
	if err != nil {
		return microerror.Mask(err)
	}

	var exists bool
	f.Token, exists = os.LookupEnv("DEVCTL_GITHUB_ACCESS_TOKEN")
	if !exists {
		return microerror.Mask(err)
	}
	ctx := context.Background()
	f.Client = github.NewClient(oauth2.NewClient(ctx, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: f.Token})))
	_, _, err = f.Client.Repositories.Get(ctx, f.Organization, f.RepositoryName)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func getAuthorFromGitConfigFile() (*object.Signature, error) {
	gitconfigfile, err := os.Open(os.Getenv("HOME") + "/.gitconfig")
	if err != nil {
		return &object.Signature{}, microerror.Mask(err)
	}

	d := formatConfig.NewDecoder(gitconfigfile)
	gitconfig := formatConfig.New()
	err = d.Decode(gitconfig)
	if err != nil {
		return &object.Signature{}, microerror.Mask(err)
	}

	userConfig := gitconfig.Section("user")
	return &object.Signature{Name: userConfig.Option("name"), Email: userConfig.Option("email"), When: time.Now()}, nil
}
