package followcircleci

import (
	"bytes"
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/circleciapp"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

// commands is a devctl command tree: the path of each leaf to its command.
func commands() map[string]*cobra.Command {
	root := &cobra.Command{Use: "devctl"}
	byPath := map[string]*cobra.Command{"": root}
	for _, path := range []string{"pr wait", "serve", "auth logout", "auth exec", "repo follow-circleci", "version", "completion zsh"} {
		parent := root
		key := ""
		for _, name := range strings.Fields(path) {
			if key != "" {
				key += " "
			}
			key += name
			c, ok := byPath[key]
			if !ok {
				c = &cobra.Command{Use: name, Run: func(*cobra.Command, []string) {}}
				parent.AddCommand(c)
				byPath[key] = c
			}
			parent = c
		}
	}
	return byPath
}

type autoFixture struct {
	auto   *autoPass
	ci     *fakeCircleCI
	stderr *bytes.Buffer
	now    time.Time
	env    map[string]string
}

func newAuto(t *testing.T, rows ...manager.Row) *autoFixture {
	t.Helper()
	f := &autoFixture{
		ci:     &fakeCircleCI{},
		stderr: &bytes.Buffer{},
		now:    time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC),
		env:    map[string]string{},
	}
	m := &fakeManager{rows: rows}
	f.auto = &autoPass{
		getenv: func(k string) string { return f.env[k] },
		state:  filepath.Join(t.TempDir(), "circleci-pending.json"),
		now:    func() time.Time { return f.now },
		stderr: f.stderr,
		pass: &pendingPass{
			open: func(context.Context) (*client.Session, error) { return &client.Session{Caller: m}, nil },
			openCI: func(context.Context, string, func(string)) (circleciapp.GitHub, circleciapp.CircleCI, error) {
				return fakeGitHub{}, f.ci, nil
			},
		},
	}
	return f
}

var newService = manager.Row{Repository: "giantswarm/new-service", Team: "team-honeybadger"}

func TestAuto_SetsUpAfterACommandAndSaysWhatItCreated(t *testing.T) {
	f := newAuto(t, newService)

	f.auto.run(t.Context(), commands()["pr wait"])

	require.Equal(t, []string{"definition new-service-app", "trigger all-pushes", "run branch main"}, f.ci.writes)
	require.Equal(t, "devctl: giantswarm/new-service: created the GitHub App pipeline, ran branch main (pipeline 1)\n", f.stderr.String())
}

func TestAuto_RunsAtMostOnceEverySixHours(t *testing.T) {
	f := newAuto(t, newService)
	f.auto.run(t.Context(), commands()["pr wait"])

	f.ci = &fakeCircleCI{}
	f.now = f.now.Add(5 * time.Hour)
	f.auto.run(t.Context(), commands()["pr wait"])
	require.Empty(t, f.ci.writes, "within six hours of the last pass")

	f.now = f.now.Add(2 * time.Hour)
	f.auto.run(t.Context(), commands()["pr wait"])
	require.NotEmpty(t, f.ci.writes, "six hours after the last pass")
}

func TestAuto_SkipsTheCommandsItMustNotFollow(t *testing.T) {
	for _, path := range []string{"", "serve", "auth logout", "auth exec", "repo follow-circleci", "version", "completion zsh"} {
		t.Run(path, func(t *testing.T) {
			f := newAuto(t, newService)
			f.auto.run(t.Context(), commands()[path])
			require.Empty(t, f.ci.writes)
			require.Empty(t, f.stderr.String())
		})
	}
}

func TestAuto_SkipsAHelpRequest(t *testing.T) {
	f := newAuto(t, newService)
	cmd := commands()["pr wait"]
	cmd.InitDefaultHelpFlag()
	require.NoError(t, cmd.Flags().Parse([]string{"--help"}))

	f.auto.run(t.Context(), cmd)

	require.Empty(t, f.ci.writes)
}

func TestAuto_TheEnvironmentTurnsItOff(t *testing.T) {
	f := newAuto(t, newService)
	f.env[OptOutEnv] = "1"

	f.auto.run(t.Context(), commands()["pr wait"])

	require.Empty(t, f.ci.writes)
}

func TestAuto_SilentWithoutAMusterLogin(t *testing.T) {
	f := newAuto(t, newService)
	f.auto.pass.open = func(context.Context) (*client.Session, error) {
		return nil, &authstore.AuthRequiredError{Identity: "muster", Cause: "no token", Hint: "devctl auth login --muster-only"}
	}

	f.auto.run(t.Context(), commands()["pr wait"])

	require.Empty(t, f.stderr.String())
}

func TestAuto_WithoutACircleCILoginSaysOnceADayWhatToDo(t *testing.T) {
	f := newAuto(t, newService)
	f.auto.pass.openCI = func(context.Context, string, func(string)) (circleciapp.GitHub, circleciapp.CircleCI, error) {
		return nil, nil, &authstore.AuthRequiredError{Identity: "CircleCI", Cause: "the token expired", Hint: "devctl auth login --circleci-only"}
	}

	f.auto.run(t.Context(), commands()["pr wait"])
	notice := f.stderr.String()
	require.Contains(t, notice, "1 repository of your teams waits for a CircleCI pipeline (giantswarm/new-service)")
	require.Contains(t, notice, "the token expired")
	require.Contains(t, notice, "devctl auth login --circleci-only")
	require.Contains(t, notice, "grant Write")

	f.stderr.Reset()
	f.now = f.now.Add(7 * time.Hour)
	f.auto.run(t.Context(), commands()["pr wait"])
	require.Empty(t, f.stderr.String(), "the second pass of the day says nothing")

	f.now = f.now.Add(18 * time.Hour)
	f.auto.run(t.Context(), commands()["pr wait"])
	require.Contains(t, f.stderr.String(), "waits for a CircleCI pipeline", "a day after the last notice")
}

func TestAuto_SilentOnOtherFailures(t *testing.T) {
	f := newAuto(t, newService)
	f.auto.pass.openCI = func(context.Context, string, func(string)) (circleciapp.GitHub, circleciapp.CircleCI, error) {
		return fakeGitHub{}, failingProject{fakeCircleCI: f.ci, repo: "new-service"}, nil
	}

	f.auto.run(t.Context(), commands()["pr wait"])

	require.Empty(t, f.stderr.String())
}

func TestAuto_ARefusedWriteNamesBothFixes(t *testing.T) {
	f := newAuto(t, newService)
	f.ci.refuse = http.StatusForbidden

	f.auto.run(t.Context(), commands()["pr wait"])

	notice := f.stderr.String()
	require.Contains(t, notice, "1 repository of your teams waits for a CircleCI pipeline (giantswarm/new-service)")
	require.Contains(t, notice, "grant Write access")
	require.Contains(t, notice, "a repository admin has to run `devctl repo follow-circleci giantswarm/new-service`")
	require.Equal(t, 1, strings.Count(notice, "\n"), "one line")
}
