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
	ChangelogFile              = "CHANGELOG.md"
	VersionFile                = "pkg/project/project.go"
	WorkInProgressSuffix       = "-dev"
	WorkInProgressVersionRegex = `([0-9]+\.)([0-9]+\.)([0-9]+)` + WorkInProgressSuffix
)

type flag struct {
	Author                         *object.Signature
	BranchName                     string
	ChangelogFile                  string
	Client                         *github.Client
	CurrentVersion                 string
	GitConfigFile                  string
	NextPatchWorkInProgressVersion string
	Organization                   string
	RepositoryName                 string
	RepositoryPath                 string
	ReviewReleaseBeforeMerging     bool
	TagName                        string
	Token                          string
	VersionFile                    string
	WorkInProgressVersion          string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.GitConfigFile, "gitconfigfile", os.Getenv("HOME")+"/.gitconfig", `Path to the Git config file used to read user configuration.`)
	cmd.Flags().StringVar(&f.Organization, "organization", "giantswarm", `Github organization owning the repository, used to publish the Github release.`)
	cmd.Flags().StringVar(&f.RepositoryName, "repositoryname", "", `Repository name on Github. Defaults to current directory.`)
	cmd.Flags().StringVar(&f.RepositoryPath, "repositorypath", "", `Path where the git repository lives in your file system. Defaults to current directory.`)
	cmd.Flags().BoolVar(&f.ReviewReleaseBeforeMerging, "reviewreleasebeforemerging", true, `Whether or not to create a pull request to review the release. When false it will commit to master.`)
}

func (f *flag) Validate() error {
	var err error

	f.RepositoryPath = strings.TrimSuffix(f.RepositoryPath, "/")
	f.ChangelogFile = fmt.Sprintf("%s/%s", f.RepositoryPath, ChangelogFile)
	f.VersionFile = fmt.Sprintf("%s/%s", f.RepositoryPath, VersionFile)
	if f.RepositoryPath == "" {
		f.ChangelogFile = ChangelogFile
		f.VersionFile = VersionFile
	}

	f.WorkInProgressVersion, f.CurrentVersion, f.NextPatchWorkInProgressVersion, err = getVersions(f.VersionFile)
	if err != nil {
		return microerror.Mask(err)
	}

	f.TagName = fmt.Sprintf("v%s", f.CurrentVersion)
	f.BranchName = fmt.Sprintf("release-%s", f.TagName)

	if f.RepositoryName == "" {
		path, err := os.Getwd()
		if err != nil {
			return microerror.Mask(err)
		}
		f.RepositoryName = filepath.Base(path)
	}

	f.Author, err = getAuthorFromGitConfigFile(f.GitConfigFile)
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

func getAuthorFromGitConfigFile(gitconfigfile string) (*object.Signature, error) {
	configfile, err := os.Open(gitconfigfile)
	if err != nil {
		return &object.Signature{}, microerror.Mask(err)
	}

	d := formatConfig.NewDecoder(configfile)
	gitconfig := formatConfig.New()
	err = d.Decode(gitconfig)
	if err != nil {
		return &object.Signature{}, microerror.Mask(err)
	}

	userConfig := gitconfig.Section("user")
	return &object.Signature{Name: userConfig.Option("name"), Email: userConfig.Option("email"), When: time.Now()}, nil
}

func getVersions(file string) (string, string, string, error) {
	b, err := ioutil.ReadFile(file)
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
