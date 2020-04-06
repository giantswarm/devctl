package release

import (
	"context"
	"fmt"
	"io"
	"os/exec"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/google/go-github/github"
	"github.com/spf13/cobra"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
)

const VersionFile = "pkg/project/project.go"

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
	fmt.Printf("Current version: %s", r.flag.WorkInProgressVersion)
	fmt.Println()
	fmt.Printf("Releasing tag: %s", r.flag.CurrentVersion)
	fmt.Println()
	fmt.Printf("Next work in progress version: %s", r.flag.NextPatchWorkInProgressVersion)
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

	commit, err := r.replaceWorkInProgressVersionWithRelease(worktree)
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

	_, err = r.replaceReleaseVersionWithNextWorkInProgress(worktree)
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
	_, _, err = r.flag.Client.Repositories.CreateRelease(ctx, r.flag.Organization, r.flag.RepositoryName, ghrelease)
	if err != nil {
		return microerror.Mask(err)
	}

	statuses, _, err := r.flag.Client.Repositories.ListStatuses(ctx, r.flag.Organization, r.flag.RepositoryName, commit.String(), &github.ListOptions{})
	if err != nil {
		return microerror.Mask(err)
	}

	fmt.Printf("Github release '%s' created successfuly!", r.flag.TagName)
	fmt.Println()
	if len(statuses) > 0 {
		fmt.Printf("Check that the workflow containing this job ends up successfully %s", *statuses[0].TargetURL)
		fmt.Println()
	}

	return nil
}

// replaceWorkInProgressVersionWithRelease replaces work in progress version with the release that we want to publish in the source code.
func (r *runner) replaceWorkInProgressVersionWithRelease(worktree *git.Worktree) (plumbing.Hash, error) {
	err := replaceVersionInFile(r.flag.WorkInProgressVersion, r.flag.CurrentVersion)
	if err != nil {
		return plumbing.Hash{}, microerror.Mask(err)
	}

	commit, err := addAndCommitChanges(worktree, r.flag.Author, fmt.Sprintf("release %s", r.flag.TagName))
	if err != nil {
		return plumbing.Hash{}, microerror.Mask(err)
	}

	return commit, err
}

// replaceWorkInProgressVersionWithRelease replaces work in progress version with the release that we want to publish in the source code.
func (r *runner) replaceReleaseVersionWithNextWorkInProgress(worktree *git.Worktree) (plumbing.Hash, error) {
	err := replaceVersionInFile(r.flag.CurrentVersion, r.flag.NextPatchWorkInProgressVersion)
	if err != nil {
		return plumbing.Hash{}, microerror.Mask(err)
	}

	commit, err := addAndCommitChanges(worktree, r.flag.Author, fmt.Sprintf("bump new work in progress version to %s", r.flag.NextPatchWorkInProgressVersion))
	if err != nil {
		return plumbing.Hash{}, microerror.Mask(err)
	}

	return commit, err
}

func addAndCommitChanges(worktree *git.Worktree, author *object.Signature, commitMessage string) (plumbing.Hash, error) {
	_, err := worktree.Add(VersionFile)
	if err != nil {
		return plumbing.Hash{}, microerror.Mask(err)
	}

	commit, err := worktree.Commit(commitMessage, &git.CommitOptions{
		Author: author,
	})

	return commit, microerror.Mask(err)
}

func replaceVersionInFile(search, replaceWith string) error {
	sedCommand := fmt.Sprintf("s/%s/%s/", search, replaceWith)
	command := exec.Command("sed", "-i", "-E", sedCommand, VersionFile)

	return microerror.Mask(command.Run())
}
