package prmerge

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v92/github"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

// allowedMethods are the merge methods the repository's settings allow, in
// the order GitHub's merge box lists them. Nil when the settings do not say:
// the token may not read them, and GitHub judges the asked method itself.
func allowedMethods(repository *github.Repository) []githubclient.MergeMethod {
	if repository.AllowMergeCommit == nil || repository.AllowSquashMerge == nil || repository.AllowRebaseMerge == nil {
		return nil
	}
	allowed := []githubclient.MergeMethod{}
	for _, m := range []struct {
		method  githubclient.MergeMethod
		allowed bool
	}{
		{githubclient.MergeCommit, repository.GetAllowMergeCommit()},
		{githubclient.MergeSquash, repository.GetAllowSquashMerge()},
		{githubclient.MergeRebase, repository.GetAllowRebaseMerge()},
	} {
		if m.allowed {
			allowed = append(allowed, m.method)
		}
	}
	return allowed
}

// resolveMethod settles the method against the repository's settings
// into result.Method, the method the merge lands with, before the wait, so a merge GitHub would decline for its method is not
// waited for. The asked method stands when the settings allow it or do not
// say. The default squash, not allowed, gives way to the one other method
// the settings allow, with a warning; an explicit --rebase never gives way,
// and neither does squash when two others are allowed: both are exit 3,
// naming the allowed methods.
func (m *Merger) resolveMethod(ctx context.Context, owner, repo string, result *Result) error {
	repository, _, err := m.github.GitHub().Repositories.Get(ctx, owner, repo)
	if err != nil {
		return microerror.Mask(err)
	}
	allowed := allowedMethods(repository)
	if allowed == nil || slices.Contains(allowed, m.method) {
		return nil
	}
	if m.method == githubclient.MergeSquash && len(allowed) == 1 {
		w := fmt.Sprintf("%s/%s does not allow squash merges, only %s: the pull request lands with method %s", owner, repo, describe(allowed), allowed[0])
		result.Method = string(allowed[0])
		m.progress.Printf("warning: %s", w)
		result.Warnings = append(result.Warnings, w)
		return nil
	}
	reason := fmt.Sprintf("%s/%s does not allow %s merges; its settings allow %s", owner, repo, m.method, describe(allowed))
	if m.method == githubclient.MergeSquash && slices.Contains(allowed, githubclient.MergeRebase) {
		reason += "; --rebase lands a rebase merge"
	}
	return agentcli.NewExitError(agentcli.ExitNotApplicable, agentcli.VerdictNotApplicable, "%s", reason)
}

// describe names the methods the way GitHub's settings do: "merge commits,
// rebase merges".
func describe(methods []githubclient.MergeMethod) string {
	names := map[githubclient.MergeMethod]string{
		githubclient.MergeCommit: "merge commits",
		githubclient.MergeSquash: "squash merges",
		githubclient.MergeRebase: "rebase merges",
	}
	described := make([]string, 0, len(methods))
	for _, m := range methods {
		described = append(described, names[m])
	}
	return strings.Join(described, ", ")
}
