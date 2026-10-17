package releasepromote

import (
	"context"
	"fmt"

	"github.com/google/go-github/v92/github"

	"github.com/giantswarm/devctl/v8/pkg/releasewait"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

// TeamRepositories lists owner/repo of every entry of a team's file in
// giantswarm/github whose release model is auto-release (releaseWorkflow,
// defaulting from gen.ci.generate) and whose life is not over (archived or
// deleted), in file order. The repositories are in
// the organisation the team files describe. A team without a file is a
// usage error.
func TeamRepositories(ctx context.Context, gh *github.Client, team string) ([]string, error) {
	remote := reposetup.Remote{GitHub: gh}
	tf, err := remote.TeamFile(ctx, team)
	if reposetup.IsEntryNotFound(err) {
		return nil, usageErr("%v", err)
	}
	if err != nil {
		return nil, fmt.Errorf("reading the team file %s: %w", reposetup.TeamFilePath(team), err)
	}
	repositories, err := teamFileRepositories(tf.TeamFile)
	if err != nil {
		return nil, fmt.Errorf("%w in %s", err, tf.Path)
	}
	return repositories, nil
}

// teamFileRepositories lists owner/repo of the team file's auto-release
// entries whose life is not over: an archived repository takes no release,
// and a deleted one is gone from GitHub, its entry kept as the record.
func teamFileRepositories(tf *reposetup.TeamFile) ([]string, error) {
	repositories := []string{}
	for _, declaration := range tf.Entries {
		if declaration.Name == "" {
			continue
		}
		fields, err := declaration.Fields()
		if err != nil {
			return nil, fmt.Errorf("decoding the entry of %s: %w", declaration.Name, err)
		}
		if reposetup.LifecycleOver(fields.Lifecycle) {
			continue
		}
		if releasewait.EntryReleaseModel(&fields) != releasewait.ReleaseModelAutoRelease {
			continue
		}
		repositories = append(repositories, reposetup.DefaultOwner+"/"+declaration.Name)
	}
	return repositories, nil
}
