package releasepromote

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// Call is one promotion as devctl release promote and devctl serve's
// release_promote take it: named repositories or a team, and the options.
type Call struct {
	// Repositories are owner/repo each; exactly one of them and Team.
	Repositories []string
	Team         string
	DryRun       bool
	// Version is a candidate vX.Y.Z-rc.N or X.Y.Z-rc.N of the one named
	// repository; [Call.Check] puts the v in front.
	Version string
	// Branch is a branch of the one named repository.
	Branch        string
	Follow        bool
	FollowTimeout time.Duration
}

// Check refuses a call no promotion can run (exit 7) before anything is
// read, and drops repeated repositories.
func (c *Call) Check() error {
	if (len(c.Repositories) == 0) == (c.Team == "") {
		return usageErr("usage: devctl %s (<owner/repo>... | --team <team>): pass exactly one of repositories and --team, got %d repository argument(s) and --team %q", Command, len(c.Repositories), c.Team)
	}
	for _, arg := range c.Repositories {
		if _, _, ok := SplitRepository(arg); !ok {
			return usageErr("%q is not a repository: expected owner/repo", arg)
		}
	}
	c.Repositories = unique(c.Repositories)
	if c.FollowTimeout < 0 {
		return usageErr("--follow-timeout must not be negative, got %s", c.FollowTimeout)
	}
	if c.Version != "" {
		// The bare version is the tag's, as devctl release wait takes it.
		version := "v" + strings.TrimPrefix(c.Version, "v")
		if !ValidVersion(version) {
			return usageErr("--version %q is not a release candidate: expected vX.Y.Z-rc.N or X.Y.Z-rc.N", c.Version)
		}
		c.Version = version
		if len(c.Repositories) != 1 {
			return usageErr("--version names a candidate of one repository: pass exactly one owner/repo with it, got %d repository argument(s) and --team %q", len(c.Repositories), c.Team)
		}
	}
	if c.Branch != "" && len(c.Repositories) != 1 {
		return usageErr("--branch names a branch of one repository: pass exactly one owner/repo with it, got %d repository argument(s) and --team %q", len(c.Repositories), c.Team)
	}
	return nil
}

// Run promotes the call's repositories, a team's listed through sources,
// into doc, a checked call ([Call.Check]).
func (c Call) Run(ctx context.Context, sources *Sources, clock agentcli.Clock, progress *agentcli.Progress, doc *Document) error {
	doc.Identity = sources.Identity
	repositories := c.Repositories
	if c.Team != "" {
		progress.Printf("listing the auto-release repositories of %s", c.Team)
		var err error
		repositories, err = sources.Team(ctx, c.Team)
		if err != nil {
			return err
		}
		if len(repositories) == 0 {
			doc.Warn(fmt.Sprintf("the team file of %s declares no auto-release repository: nothing to promote", c.Team))
		}
	}
	return Promote(ctx, Config{
		GitHub:        sources.GitHub,
		DryRun:        c.DryRun,
		Version:       c.Version,
		Branch:        c.Branch,
		NotFoundHint:  sources.NotFoundHint,
		Follow:        c.Follow,
		FollowTimeout: c.FollowTimeout,
		Clock:         clock,
		Progress:      progress,
	}, repositories, &doc.Result)
}

// unique is repositories without repeats, in first-seen order.
func unique(repositories []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(repositories))
	for _, repository := range repositories {
		if seen[repository] {
			continue
		}
		seen[repository] = true
		out = append(out, repository)
	}
	return out
}
