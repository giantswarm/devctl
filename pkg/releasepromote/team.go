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
// defaulting from gen.ci.generate), in file order. The repositories are in
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
	repositories := []string{}
	for _, declaration := range tf.Entries {
		if declaration.Name == "" {
			continue
		}
		fields, err := declaration.Fields()
		if err != nil {
			return nil, fmt.Errorf("decoding the entry of %s in %s: %w", declaration.Name, tf.Path, err)
		}
		if releasewait.EntryReleaseModel(&fields) != releasewait.ReleaseModelAutoRelease {
			continue
		}
		repositories = append(repositories, reposetup.DefaultOwner+"/"+declaration.Name)
	}
	return repositories, nil
}
