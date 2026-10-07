package gitremote

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_Parse(t *testing.T) {
	testCases := []struct {
		name   string
		remote string
		want   Repo
	}{
		{name: "https", remote: "https://github.com/giantswarm/valkey-app", want: Repo{Host: "github.com", Owner: "giantswarm", Name: "valkey-app"}},
		{name: "https with .git", remote: "https://github.com/giantswarm/valkey-app.git", want: Repo{Host: "github.com", Owner: "giantswarm", Name: "valkey-app"}},
		{name: "https with trailing slash", remote: "https://github.com/giantswarm/valkey-app/", want: Repo{Host: "github.com", Owner: "giantswarm", Name: "valkey-app"}},
		{name: "https with a token", remote: "https://x-access-token:ghs_secret@github.com/giantswarm/valkey-app.git", want: Repo{Host: "github.com", Owner: "giantswarm", Name: "valkey-app"}}, //nolint:gosec // a fake token
		{name: "ssh scp-like", remote: "git@github.com:giantswarm/valkey-app", want: Repo{Host: "github.com", Owner: "giantswarm", Name: "valkey-app"}},
		{name: "ssh scp-like with .git", remote: "git@github.com:giantswarm/valkey-app.git", want: Repo{Host: "github.com", Owner: "giantswarm", Name: "valkey-app"}},
		{name: "ssh url with .git", remote: "ssh://git@github.com/giantswarm/valkey-app.git", want: Repo{Host: "github.com", Owner: "giantswarm", Name: "valkey-app"}},
		{name: "ssh url with port", remote: "ssh://git@github.com:22/giantswarm/valkey-app.git", want: Repo{Host: "github.com", Owner: "giantswarm", Name: "valkey-app"}},
		{name: "ssh host alias", remote: "github-work:giantswarm/valkey-app.git", want: Repo{Host: "github-work", Owner: "giantswarm", Name: "valkey-app"}},
		{name: "dot-prefixed name", remote: "https://github.com/giantswarm/.github", want: Repo{Host: "github.com", Owner: "giantswarm", Name: ".github"}},
		{name: "non-giantswarm owner", remote: "git@github.com:teemow/valkey-app.git", want: Repo{Host: "github.com", Owner: "teemow", Name: "valkey-app"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.remote)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func Test_Parse_Invalid(t *testing.T) {
	testCases := []struct {
		name   string
		remote string
	}{
		{name: "empty", remote: ""},
		{name: "absolute local path", remote: "/home/user/valkey-app"},
		{name: "relative local path", remote: "../valkey-app"},
		{name: "file url", remote: "file:///srv/git/valkey-app.git"},
		{name: "owner only", remote: "https://github.com/giantswarm"},
		{name: "owner only with a token", remote: "https://x-access-token:ghs_secret@github.com/giantswarm"}, //nolint:gosec // a fake token
		{name: "path below the repository", remote: "https://github.com/giantswarm/valkey-app/tree/main"},
		{name: "scp-like without a path", remote: "git@github.com:"},
		{name: "unparsable url with a token", remote: "https://x-access-token:ghs_secret@github.com:port/giantswarm/valkey-app"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.remote)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "ghs_secret")
		})
	}
}

// Test_RepoName runs git in a checkout whose directory is not named after
// its repository, the way a worktree is.
func Test_RepoName(t *testing.T) {
	testCases := []struct {
		name    string
		remote  string
		want    string
		wantErr string
	}{
		{name: "https", remote: "https://github.com/giantswarm/valkey-app.git", want: "valkey-app"},
		{name: "ssh", remote: "git@github.com:giantswarm/valkey-app.git", want: "valkey-app"},
		{name: "owner in another case", remote: "https://github.com/GiantSwarm/valkey-app", want: "valkey-app"},
		{name: "non-giantswarm owner", remote: "git@github.com:teemow/valkey-app.git", wantErr: "github.com/teemow/valkey-app is not a giantswarm repository"},
		{name: "no origin remote", wantErr: "git remote get-url origin"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "valkey-app-79")
			git(t, "init", "--quiet", dir)
			if tc.remote != "" {
				git(t, "-C", dir, "remote", "add", "origin", tc.remote)
			}

			got, err := RepoName(context.Background(), dir)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func Test_RepoName_NoCheckout(t *testing.T) {
	_, err := RepoName(context.Background(), t.TempDir())
	require.ErrorContains(t, err, "git remote get-url origin")
}

func git(t *testing.T, args ...string) {
	t.Helper()

	out, err := exec.Command("git", args...).CombinedOutput() //nolint:gosec // the arguments are the test's own
	require.NoError(t, err, string(out))
}
