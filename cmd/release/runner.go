package release

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"regexp"
	"strings"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/google/go-github/github"
	"github.com/spf13/cobra"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
)

type runner struct {
	flag   *flag
	logger micrologger.Logger
	stdout io.Writer
	stderr io.Writer
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	err := r.flag.Validate()
	if err != nil {
		return microerror.Mask(err)
	}

	err = r.run(ctx, cmd, args)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (r *runner) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	var err error

	fmt.Printf("Creating a new release for %s/%s", r.flag.Organization, r.flag.RepositoryName)
	fmt.Println()
	fmt.Println()
	fmt.Printf("Current version: %s", r.flag.WorkInProgressVersion)
	fmt.Println()
	fmt.Printf("Releasing tag: %s", r.flag.TagName)
	fmt.Println()
	fmt.Printf("Next Work In Progress version: %s", r.flag.NextPatchWorkInProgressVersion)
	fmt.Println()
	fmt.Println()

	repo, err := git.PlainOpen(r.flag.RepositoryPath)
	if err != nil {
		return microerror.Mask(err)
	}

	worktree, err := repo.Worktree()
	if err != nil {
		return microerror.Mask(err)
	}

	_, err = r.replaceWorkInProgressVersionWithRelease(fmt.Sprintf("%s/%s", r.flag.RepositoryPath, VersionFile), worktree)
	if err != nil {
		return microerror.Mask(err)
	}

	commit, err := r.addReleaseToChangelog(fmt.Sprintf("%s/%s", r.flag.RepositoryPath, ChangelogFile), worktree)
	if err != nil {
		return microerror.Mask(err)
	}

	_, err = repo.CreateTag(r.flag.TagName, commit, &git.CreateTagOptions{
		Tagger:  r.flag.Author,
		Message: r.flag.TagName,
	})
	if err != nil {
		return microerror.Mask(err)
	}

	_, err = r.replaceReleaseVersionWithNextWorkInProgress(fmt.Sprintf("%s/%s", r.flag.RepositoryPath, VersionFile), worktree)
	if err != nil {
		return microerror.Mask(err)
	}

	err = repo.PushContext(ctx, &git.PushOptions{
		RefSpecs: []config.RefSpec{
			"refs/heads/*:refs/heads/*",
			"refs/tags/*:refs/tags/*",
		},
	})
	if err != nil {
		return microerror.Mask(err)
	}

	ghrelease := &github.RepositoryRelease{
		TagName: github.String(r.flag.TagName),
	}
	ghrepositoryRelease, _, err := r.flag.Client.Repositories.CreateRelease(ctx, r.flag.Organization, r.flag.RepositoryName, ghrelease)
	if err != nil {
		return microerror.Mask(err)
	}

	statuses, _, err := r.flag.Client.Repositories.ListStatuses(ctx, r.flag.Organization, r.flag.RepositoryName, commit.String(), &github.ListOptions{})
	if err != nil {
		return microerror.Mask(err)
	}

	fmt.Printf("Github release '%s' created successfully! %s", r.flag.TagName, *ghrepositoryRelease.HTMLURL)
	fmt.Println()
	if len(statuses) > 0 {
		fmt.Printf("Check that the workflow containing this job ends up successfully %s", *statuses[0].TargetURL)
		fmt.Println()
		fmt.Println()
	}

	return nil
}

// replaceWorkInProgressVersionWithRelease replaces work in progress version with the release that we want to publish in the source code.
func (r *runner) replaceWorkInProgressVersionWithRelease(file string, worktree *git.Worktree) (plumbing.Hash, error) {
	err := r.replaceVersionInFile(file, r.flag.WorkInProgressVersion, r.flag.CurrentVersion)
	if err != nil {
		return plumbing.Hash{}, microerror.Mask(err)
	}

	commit, err := r.addAndCommitChanges(file, worktree, r.flag.Author, fmt.Sprintf("release %s", r.flag.TagName))
	if err != nil {
		return plumbing.Hash{}, microerror.Mask(err)
	}

	return commit, err
}

// replaceWorkInProgressVersionWithRelease replaces work in progress version with the release that we want to publish in the source code.
func (r *runner) replaceReleaseVersionWithNextWorkInProgress(file string, worktree *git.Worktree) (plumbing.Hash, error) {
	err := r.replaceVersionInFile(file, r.flag.CurrentVersion, r.flag.NextPatchWorkInProgressVersion)
	if err != nil {
		return plumbing.Hash{}, microerror.Mask(err)
	}

	commit, err := r.addAndCommitChanges(file, worktree, r.flag.Author, fmt.Sprintf("bump new work in progress version to %s", r.flag.NextPatchWorkInProgressVersion))
	if err != nil {
		return plumbing.Hash{}, microerror.Mask(err)
	}

	return commit, err
}

func (r *runner) addAndCommitChanges(file string, worktree *git.Worktree, author *object.Signature, commitMessage string) (plumbing.Hash, error) {
	_, err := worktree.Add(strings.TrimPrefix(file, r.flag.RepositoryPath+"/"))
	if err != nil {
		return plumbing.Hash{}, microerror.Mask(err)
	}

	commit, err := worktree.Commit(commitMessage, &git.CommitOptions{
		Author: author,
	})

	return commit, microerror.Mask(err)
}

func (r *runner) replaceVersionInFile(file, search, replaceWith string) error {
	f, err := ioutil.ReadFile(file)
	if err != nil {
		return microerror.Mask(err)
	}
	filecontents := string(f)

	if !strings.Contains(filecontents, search) {
		return microerror.Maskf(NoVersionFoundInFileError, "No version was found in %s", file)
	}

	updatedFileContents := []byte(strings.Replace(filecontents, search, replaceWith, 1))
	err = ioutil.WriteFile(file, updatedFileContents, 0)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (r *runner) addReleaseToChangelog(file string, worktree *git.Worktree) (plumbing.Hash, error) {
	search := "## [Unreleased]"
	replaceWith := fmt.Sprintf("## [Unreleased]\n\n## [%s] %s", r.flag.CurrentVersion, time.Now().Format("2006-01-02"))
	f, err := ioutil.ReadFile(file)
	if err != nil {
		return plumbing.Hash{}, microerror.Mask(err)
	}
	filecontents := string(f)

	if !strings.Contains(filecontents, search) {
		return plumbing.Hash{}, microerror.Maskf(NoUnreleasedWorkFoundInChangelogError, "No unreleased work was found in %s", file)
	}

	updatedFileContents := []byte(strings.Replace(filecontents, search, replaceWith, 1))
	err = ioutil.WriteFile(file, updatedFileContents, 0)
	if err != nil {
		return plumbing.Hash{}, microerror.Mask(err)
	}

	// Change [Unreleased] link
	m1 := regexp.MustCompile(`(\[Unreleased]:)(.*)(v[0-9]+\.[0-9]+\.[0-9]+)`)
	updatedFileContents = []byte(m1.ReplaceAllString(string(updatedFileContents), fmt.Sprintf("$1${2}%s$5", r.flag.TagName)))
	err = ioutil.WriteFile(file, updatedFileContents, 0)
	if err != nil {
		return plumbing.Hash{}, microerror.Mask(err)
	}

	// Change new tag's link
	taglink := fmt.Sprintf("https://github.com/%s/%s/releases/tag/%s", r.flag.Organization, r.flag.RepositoryName, r.flag.TagName)
	m2 := regexp.MustCompile(`(\[Unreleased]:.*)`)
	updatedFileContents = []byte(m2.ReplaceAllString(string(updatedFileContents), fmt.Sprintf("${1}\n[%s]: %s", r.flag.CurrentVersion, taglink)))
	err = ioutil.WriteFile(file, updatedFileContents, 0)
	if err != nil {
		return plumbing.Hash{}, microerror.Mask(err)
	}

	commit, err := r.addAndCommitChanges("CHANGELOG.md", worktree, r.flag.Author, fmt.Sprintf("add release %s to changelog", r.flag.CurrentVersion))
	if err != nil {
		return plumbing.Hash{}, microerror.Mask(err)
	}

	return commit, nil
}
