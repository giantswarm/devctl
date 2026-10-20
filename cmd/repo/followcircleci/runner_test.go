package followcircleci

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v93/github"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

// fakeCircleCI records the calls; the follow installs the hook when
// installs is set.
type fakeCircleCI struct {
	following bool
	notFound  bool
	installs  bool
	hook      *bool
	calls     []string
}

func (f *fakeCircleCI) Me(context.Context) (*circleciclient.User, error) {
	return &circleciclient.User{Login: "alice"}, nil
}

func (f *fakeCircleCI) Following(context.Context, string, string) (bool, error) {
	f.calls = append(f.calls, "following")
	if f.notFound {
		// CircleCI's own answer for a project it does not know.
		srv := httptest.NewServer(http.NotFoundHandler())
		defer srv.Close()
		c, err := circleciclient.New(circleciclient.Config{Token: "t", BaseURL: srv.URL})
		if err != nil {
			return false, err
		}
		return c.Following(context.Background(), "giantswarm", "mcp-template")
	}
	return f.following, nil
}

func (f *fakeCircleCI) Unfollow(context.Context, string, string) error {
	f.calls = append(f.calls, "unfollow")
	f.following = false
	return nil
}

func (f *fakeCircleCI) Follow(context.Context, string, string) error {
	f.calls = append(f.calls, "follow")
	f.following = true
	if f.installs {
		*f.hook = true
	}
	return nil
}

func circleCIHook(active bool, events ...string) *github.Hook {
	return &github.Hook{Active: new(active), Events: events, Config: &github.HookConfig{URL: new("https://circleci.com/hooks/github")}}
}

func TestRun(t *testing.T) {
	tests := []struct {
		name      string
		arg       string
		hook      bool
		following bool
		notFound  bool
		installs  bool
		wantCalls []string
		wantOut   string
		wantErr   *microerror.Error
	}{
		{
			name:    "webhook present: nothing done",
			arg:     "mcp-template",
			hook:    true,
			wantOut: "giantswarm/mcp-template: webhook present, nothing to do\n",
		},
		{
			name:      "not followed by the person: the follow installs the hook",
			arg:       "giantswarm/mcp-template",
			installs:  true,
			wantCalls: []string{"following", "follow"},
			wantOut:   "giantswarm/mcp-template: followed as alice\ngiantswarm/mcp-template: webhook present\n",
		},
		{
			name:      "followed by the person already: unfollow, then follow",
			arg:       "mcp-template",
			following: true,
			installs:  true,
			wantCalls: []string{"following", "unfollow", "follow"},
			wantOut:   "giantswarm/mcp-template: unfollowed as alice\ngiantswarm/mcp-template: followed as alice\ngiantswarm/mcp-template: webhook present\n",
		},
		{
			name:      "the follow installs nothing: refused naming the login",
			arg:       "mcp-template",
			wantCalls: []string{"following", "follow"},
			wantErr:   webhookMissingError,
		},
		{
			name:      "unknown to CircleCI: nothing followed",
			arg:       "mcp-template",
			notFound:  true,
			wantCalls: []string{"following"},
			wantErr:   notFollowedError,
		},
		{
			name:    "malformed argument",
			arg:     "giantswarm/",
			wantErr: invalidArgError,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hook := tc.hook
			ci := &fakeCircleCI{following: tc.following, notFound: tc.notFound, installs: tc.installs, hook: &hook}
			var out bytes.Buffer
			sleeps := 0
			r := &runner{
				stdout: &out,
				open: func(_ context.Context, owner string) (hooks, circleCI, error) {
					require.Equal(t, "giantswarm", owner)
					read := func(context.Context, string, string) ([]*github.Hook, error) {
						hooks := []*github.Hook{circleCIHook(false, "push"), circleCIHook(true, "pull_request")}
						if hook {
							hooks = append(hooks, circleCIHook(true, "push", "pull_request"))
						}
						return hooks, nil
					}
					return read, ci, nil
				},
				sleep:    func(context.Context, time.Duration) error { sleeps++; return nil },
				wait:     time.Minute,
				interval: 5 * time.Second,
			}
			err := r.run(context.Background(), tc.arg)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.wantOut, out.String())
			}
			require.Equal(t, tc.wantCalls, ci.calls)
			if tc.wantErr == webhookMissingError {
				require.Equal(t, 12, sleeps, "the hook is read for a minute, every five seconds")
				require.Contains(t, err.Error(), "followed as alice")
			}
		})
	}
}
