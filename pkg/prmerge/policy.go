package prmerge

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v92/github"

	"github.com/giantswarm/devctl/v8/pkg/gen"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

// AgentMergeField is the team-file entry field that opts a repository out
// of agent merges.
const AgentMergeField = "agentMerge"

// Verdict is the repository's say on agent merges.
type Verdict struct {
	// Refusal is empty when the merge is allowed; otherwise the sentence
	// exit 5 carries.
	Refusal string
	// Team is the slug of the team whose file declares the repository, the
	// ruleset's bypass actor for people; empty when no team file does.
	Team string
	// Entry names the entry that declares the repository and where it is
	// held ("the entry r in repositories/team-x.yaml of giantswarm/github"),
	// the place its alignment is opted in; empty when no team file does.
	Entry string
	// ForkLine: the entry lists the fork flavour in gen.flavours, the
	// classification the generators read: the repository carries its
	// upstream's files plus one commit per carried patch, so the default
	// merge method is a rebase.
	ForkLine bool
	// Warning is why the entry's flavours could not be read; the repository
	// is then not taken for a fork line. Empty when they could.
	Warning string
}

// Policy is the repository's say on agent merges: an empty refusal allows
// the merge, a non-empty one is the sentence exit 5 carries.
type Policy func(ctx context.Context, owner, repo string) (Verdict, error)

// TeamFilePolicy reads the repository's entry the way pkg/reposetup reads
// one: the team files of giantswarm/github at main, the entry named after
// the repository. An entry with `agentMerge: false` refuses; an entry
// without the field, a repository no team file declares and a repository
// outside the organisation the team files declare are not opted out. The
// verdict names the team whose file declares the entry and says whether
// its gen.flavours make it a fork line; flavours that do not decode leave
// it no fork line, with a warning. Team files the token cannot read are an
// error: the opt-out is not guessed.
func TeamFilePolicy(gh *github.Client) Policy {
	return func(ctx context.Context, owner, repo string) (Verdict, error) {
		if !strings.EqualFold(owner, reposetup.DefaultOwner) {
			return Verdict{}, nil
		}
		remote := reposetup.Remote{GitHub: gh}
		tf, err := remote.FindEntry(ctx, repo, nil)
		if reposetup.IsEntryNotFound(err) {
			return Verdict{}, nil
		}
		if err != nil {
			return Verdict{}, microerror.Mask(err)
		}
		verdict := Verdict{Team: tf.Team, Entry: fmt.Sprintf("the entry %s in %s of %s", repo, tf.Path, remote.Slug())}
		entry, _ := tf.Entry(repo)
		instance, err := entry.Instance()
		if err != nil {
			return Verdict{}, microerror.Mask(err)
		}
		fields, _ := instance.(map[string]any)
		if allowed, ok := fields[AgentMergeField].(bool); ok && !allowed {
			verdict.Refusal = fmt.Sprintf("%s says %s: false: agents do not merge in this repository", verdict.Entry, AgentMergeField)
		}
		declared, err := entry.Fields()
		switch {
		case err != nil:
			verdict.Warning = fmt.Sprintf("the gen.flavours of %s could not be read (%v): the repository is not taken for a fork line", verdict.Entry, err)
		case declared.Gen != nil:
			verdict.ForkLine = slices.Contains(declared.Gen.Flavours, gen.FlavourFork.String())
		}
		return verdict, nil
	}
}
