package release

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/coreos/go-semver/semver"
	"github.com/giantswarm/microerror"
	"github.com/google/go-github/github"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
	formatConfig "gopkg.in/src-d/go-git.v4/plumbing/format/config"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
)

const (
	WorkInProgressSuffix       = "-dev"
	WorkInProgressVersionRegex = `([0-9]+\.)([0-9]+\.)([0-9]+)` + WorkInProgressSuffix
)

type flag struct {
	Author                         *object.Signature
	Client                         *github.Client
	CurrentVersion                 string
	NextPatchWorkInProgressVersion string
	Organization                   string
	RepositoryName                 string
	RepositoryPath                 string
	TagName                        string
	Token                          string
	WorkInProgressVersion          string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.Organization, "organization", "giantswarm", `Github organization owning the repository.`)
	cmd.Flags().StringVar(&f.RepositoryName, "repositoryName", "", `Repository name on Github. Defaults to current directory.`)
	cmd.Flags().StringVar(&f.RepositoryPath, "repositoryPath", "", `Path where the git repository lives in your file system.`)
}

func (f *flag) Validate() error {
	var err error

	f.WorkInProgressVersion, f.CurrentVersion, f.NextPatchWorkInProgressVersion, err = getVersions()
	if err != nil {
		return microerror.Mask(err)
	}

	f.TagName = fmt.Sprintf("v%s", f.CurrentVersion)

	if f.RepositoryName == "" {
		path, err := os.Getwd()
		if err != nil {
			return microerror.Mask(err)
		}
		f.RepositoryName = filepath.Base(path)
	}

	f.Author, err = getAuthorFromGitConfigFile()
	if err != nil {
		return microerror.Mask(err)
	}

	var exists bool
	f.Token, exists = os.LookupEnv("DEVCTL_GITHUB_ACCESS_TOKEN")
	if !exists {
		return microerror.Maskf(tokenNotFoundError, "You need to export 'DEVCTL_GITHUB_ACCESS_TOKEN' to access Github repositories")
	}
	ctx := context.Background()
	f.Client = github.NewClient(oauth2.NewClient(ctx, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: f.Token})))
	_, _, err = f.Client.Repositories.Get(ctx, f.Organization, f.RepositoryName)
	if err != nil {
		return microerror.Maskf(unreachableRepositoryError, "Repository can't be reached. Does the token has enough permissions?")
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

func getVersions() (string, string, string, error) {
	b, err := ioutil.ReadFile(VersionFile)
	if err != nil {
		return "", "", "", microerror.Mask(err)
	}
	re := regexp.MustCompile(WorkInProgressVersionRegex)
	matches := re.FindStringSubmatch(string(b))
	if len(matches) < 1 {
		return "", "", "", microerror.Mask(wrongNumberOfVersionsFoundError)
	}

	currentWorkInProgressVersion := matches[0]
	currentVersion := strings.TrimSuffix(currentWorkInProgressVersion, WorkInProgressSuffix)
	nextPatchVersion := semver.New(currentVersion)
	nextPatchVersion.BumpPatch()
	nextPatchWorkInProgressVersion := fmt.Sprintf("%s%s", nextPatchVersion, WorkInProgressSuffix)

	return currentWorkInProgressVersion, currentVersion, nextPatchWorkInProgressVersion, nil
}
