package releasewait

import (
	"context"
	"fmt"

	"github.com/google/go-github/v92/github"

	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

// TeamFileEntries finds a repository's declaration in the team files of the
// organisation's github repository, read as the caller. Only the
// organisation the team files describe is searched; a repository of
// another owner has no entry.
type TeamFileEntries struct {
	// GitHub is the go-github client the team files are read with.
	GitHub *github.Client
	// Owner is the organisation whose team files are searched; empty means
	// the default one.
	Owner string
}

// FindEntry implements [EntryFinder].
func (t TeamFileEntries) FindEntry(ctx context.Context, owner, repo string) (*reposetup.Fields, bool, error) {
	declaredOwner := t.Owner
	if declaredOwner == "" {
		declaredOwner = reposetup.DefaultOwner
	}
	if owner != declaredOwner {
		return nil, false, nil
	}
	remote := reposetup.Remote{GitHub: t.GitHub, Owner: declaredOwner}
	tf, err := remote.FindEntry(ctx, repo, nil)
	if reposetup.IsEntryNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	declaration, ok := tf.Entry(repo)
	if !ok {
		return nil, false, nil
	}
	fields, err := declaration.Fields()
	if err != nil {
		return nil, false, fmt.Errorf("decoding the entry of %s in %s: %w", repo, tf.Path, err)
	}
	return &fields, true, nil
}
