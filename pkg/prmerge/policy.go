package prmerge

import (
	"context"
	"fmt"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v92/github"

	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

// AgentMergeField is the team-file entry field that opts a repository out
// of agent merges.
const AgentMergeField = "agentMerge"

// Policy is the repository's say on agent merges: an empty refusal allows
// the merge, a non-empty one is the sentence exit 5 carries.
type Policy func(ctx context.Context, owner, repo string) (refusal string, err error)

// TeamFilePolicy reads the repository's entry the way pkg/reposetup reads
// one: the team files of giantswarm/github at main, the entry named after
// the repository. An entry with `agentMerge: false` refuses; an entry
// without the field, a repository no team file declares and a repository
// outside the organisation the team files declare are not opted out. Team
// files the token cannot read are an error: the opt-out is not guessed.
func TeamFilePolicy(gh *github.Client) Policy {
	return func(ctx context.Context, owner, repo string) (string, error) {
		if !strings.EqualFold(owner, reposetup.DefaultOwner) {
			return "", nil
		}
		remote := reposetup.Remote{GitHub: gh}
		tf, err := remote.FindEntry(ctx, repo, nil)
		if reposetup.IsEntryNotFound(err) {
			return "", nil
		}
		if err != nil {
			return "", microerror.Mask(err)
		}
		entry, _ := tf.Entry(repo)
		instance, err := entry.Instance()
		if err != nil {
			return "", microerror.Mask(err)
		}
		fields, _ := instance.(map[string]any)
		if allowed, ok := fields[AgentMergeField].(bool); ok && !allowed {
			return fmt.Sprintf("the entry %s in %s of %s says %s: false: agents do not merge in this repository", repo, tf.Path, remote.Slug(), AgentMergeField), nil
		}
		return "", nil
	}
}
