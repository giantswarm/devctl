package authexec

import "testing"

func TestGHOwner(t *testing.T) {
	const gsRemotes = "remote.origin.url git@github.com:giantswarm/devctl.git\n"
	const forkRemotes = "remote.origin.url https://github.com/teemow/devctl.git\nremote.upstream.url https://github.com/giantswarm/devctl\n"
	const defaultRemotes = "remote.origin.url git@github.com:giantswarm/devctl.git\nremote.mine.url git@github.com:teemow/devctl.git\nremote.mine.gh-resolved base\n"

	cases := []struct {
		name    string
		args    []string
		env     []string
		remotes string
		want    string
	}{
		{name: "--repo", args: []string{"pr", "list", "--repo", "teemow/klaus-lab"}, remotes: gsRemotes, want: "teemow"},
		{name: "-R", args: []string{"issue", "view", "1", "-R", "giantswarm/devctl"}, want: "giantswarm"},
		{name: "--repo=", args: []string{"pr", "view", "1", "--repo=Teemow/klaus-lab"}, want: "teemow"},
		{name: "-Rvalue", args: []string{"pr", "view", "1", "-Rteemow/klaus-lab"}, want: "teemow"},
		{name: "host/owner/repo", args: []string{"pr", "view", "1", "-R", "github.com/teemow/klaus-lab"}, want: "teemow"},
		{name: "after --", args: []string{"api", "user", "--", "--repo", "teemow/x"}, want: ""},
		{name: "GH_REPO", args: []string{"pr", "list"}, env: []string{"GH_REPO=teemow/klaus-lab"}, remotes: gsRemotes, want: "teemow"},
		{name: "api repos", args: []string{"api", "repos/teemow/klaus-lab/pulls"}, remotes: gsRemotes, want: "teemow"},
		{name: "api /orgs", args: []string{"api", "-X", "GET", "/orgs/giantswarm/teams"}, want: "giantswarm"},
		{name: "api {owner}", args: []string{"api", "repos/{owner}/{repo}/pulls"}, remotes: forkRemotes, want: "giantswarm"},
		{name: "api user", args: []string{"api", "user"}, remotes: forkRemotes, want: ""},
		{name: "api graphql", args: []string{"api", "graphql", "-f", "query=..."}, remotes: forkRemotes, want: ""},
		{name: "pr URL", args: []string{"pr", "view", "https://github.com/teemow/klaus-lab/pull/5"}, remotes: gsRemotes, want: "teemow"},
		{name: "repo view owner/repo", args: []string{"repo", "view", "teemow/klaus-lab"}, want: "teemow"},
		{name: "body URL is no repository", args: []string{"pr", "comment", "5", "--body", "https://github.com/teemow/x"}, remotes: gsRemotes, want: "giantswarm"},
		{name: "remote origin", args: []string{"pr", "list"}, remotes: gsRemotes, want: "giantswarm"},
		{name: "upstream before origin", args: []string{"pr", "list"}, remotes: forkRemotes, want: "giantswarm"},
		{name: "set-default wins", args: []string{"pr", "list"}, remotes: defaultRemotes, want: "teemow"},
		{name: "outside a repository", args: []string{"search", "prs", "x"}, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			remotes := func() string { return tc.remotes }
			if got := GHOwner(tc.args, tc.env, remotes); got != tc.want {
				t.Fatalf("GHOwner(%v) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}
