package owner

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

const inventoryPath = "/repos/giantswarm/github/contents/repositories"

// inventoryTransport never reaches the network. The App's repository-teams
// endpoint is forbidden, while its Contents reads succeed.
type inventoryTransport struct {
	t      *testing.T
	status map[string]int
	files  map[string]string
	paths  []string
}

func (f *inventoryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	f.paths = append(f.paths, req.URL.Path)
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	require.Equal(f.t, http.MethodGet, req.Method)
	require.Equal(f.t, "main", req.URL.Query().Get("ref"))
	require.Equal(f.t, "Bearer synthetic-owner-test", req.Header.Get("Authorization"))

	status := f.status[req.URL.Path]
	var body any
	switch {
	case status != 0:
		body = map[string]string{"message": "Resource not accessible by integration"}
	case req.URL.Path == inventoryPath:
		status = http.StatusOK
		body = []map[string]string{
			{"type": "file", "name": "team-first.yaml"},
			{"type": "file", "name": "README.md"},
			{"type": "dir", "name": "nested.yaml"},
			{"type": "file", "name": "team-owner.yaml"},
		}
	case strings.HasPrefix(req.URL.Path, inventoryPath+"/"):
		content, ok := f.files[strings.TrimPrefix(req.URL.Path, inventoryPath+"/")]
		require.True(f.t, ok, "unexpected file read: %s", req.URL.Path)
		status = http.StatusOK
		body = map[string]string{"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(content))}
	default:
		f.t.Errorf("unexpected request: %s", req.URL.Path)
		status = http.StatusForbidden
		body = map[string]string{"message": "Resource not accessible by integration"}
	}
	data, err := json.Marshal(body)
	require.NoError(f.t, err)
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(data)), Request: req}, nil
}

func setupInventory(t *testing.T) *inventoryTransport {
	t.Helper()
	// Any accidental keychain access fails below a regular file; every
	// request is handled in memory with a synthetic token.
	blocked := filepath.Join(t.TempDir(), "blocked")
	require.NoError(t, os.WriteFile(blocked, nil, 0o600))
	t.Setenv(agentcli.EnvKeyringFile, filepath.Join(blocked, "keyring.json"))
	t.Setenv("CI", "true")
	t.Setenv("DEVCTL_GITHUB_TOKEN", "synthetic-owner-test")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("OPSCTL_GITHUB_TOKEN", "")
	f := &inventoryTransport{
		t:      t,
		status: map[string]int{"/repos/giantswarm/devctl/teams": http.StatusForbidden},
		files: map[string]string{
			"team-first.yaml": "- name: another-repository\n",
			"team-owner.yaml": "- name: devctl\n",
		},
	}
	previous := http.DefaultTransport
	http.DefaultTransport = f
	t.Cleanup(func() { http.DefaultTransport = previous })
	return f
}

func execute(ctx context.Context, out io.Writer, args ...string) error {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cmd, err := New(Config{Logger: logger, Stdout: out, Stderr: io.Discard})
	if err != nil {
		return err
	}
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs(args)
	return cmd.ExecuteContext(ctx)
}

func Test_OwnerUsesTeamFilesWhenRepositoryTeamsAreForbidden(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		json bool
	}{
		{name: "case 0: bare repository", args: []string{"devctl"}},
		{name: "case 1: qualified repository", args: []string{"giantswarm/devctl"}},
		{name: "case 2: JSON with provenance", args: []string{"GIANTSWARM/devctl", "-o", "json"}, json: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setupInventory(t)
			var out bytes.Buffer
			require.NoError(t, execute(t.Context(), &out, tc.args...))
			if tc.json {
				require.JSONEq(t, `{"repository":"giantswarm/devctl","team":"team-owner","source":"https://github.com/giantswarm/github/blob/main/repositories/team-owner.yaml"}`, out.String())
			} else {
				require.Equal(t, "team-owner\n", out.String())
			}
			require.Equal(t, []string{inventoryPath, inventoryPath + "/team-first.yaml", inventoryPath + "/team-owner.yaml"}, f.paths)
		})
	}
}

func Test_OwnerDoesNotGuessOrFallBack(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(*inventoryTransport)
		match   func(error) bool
	}{
		{name: "case 0: repository undeclared", prepare: func(f *inventoryTransport) { f.files["team-owner.yaml"] = "[]" }, match: reposetup.IsEntryNotFound},
		{name: "case 1: directory unreadable", prepare: func(f *inventoryTransport) { f.status[inventoryPath] = http.StatusForbidden }},
		{name: "case 2: an earlier team file unreadable", prepare: func(f *inventoryTransport) { f.status[inventoryPath+"/team-first.yaml"] = http.StatusForbidden }},
		{name: "case 3: team file malformed", prepare: func(f *inventoryTransport) { f.files["team-first.yaml"] = "not: a-list" }, match: reposetup.IsInvalidTeamFile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setupInventory(t)
			tc.prepare(f)
			var out bytes.Buffer
			err := execute(t.Context(), &out, "devctl", "-o", "json")
			require.Error(t, err)
			if tc.match != nil {
				require.True(t, tc.match(err), "%v", err)
			} else {
				require.Contains(t, err.Error(), "403")
			}
			require.Empty(t, out.String())
			for _, path := range f.paths {
				require.True(t, path == inventoryPath || strings.HasPrefix(path, inventoryPath+"/"), "%s", path)
			}
		})
	}
}

func Test_OwnerRejectsInvalidCallsBeforeReadingInventory(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "case 0: another owner with the same repository name", args: []string{"another-org/devctl"}},
		{name: "case 1: malformed repository", args: []string{"giantswarm/devctl/extra"}},
		{name: "case 2: missing name", args: []string{"giantswarm/"}},
		{name: "case 3: missing argument"},
		{name: "case 4: extra argument", args: []string{"devctl", "extra"}},
		{name: "case 5: invalid output", args: []string{"devctl", "-o", "yaml"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setupInventory(t)
			t.Setenv("DEVCTL_GITHUB_TOKEN", "")
			var out bytes.Buffer
			err := execute(t.Context(), &out, tc.args...)
			require.Error(t, err)
			require.NotErrorIs(t, err, authstore.ErrAuthRequired, "arguments are validated before authentication")
			require.Empty(t, out.String())
			require.Empty(t, f.paths)
		})
	}
}

func Test_OwnerAuthenticationAndCancellation(t *testing.T) {
	t.Run("case 0: missing authentication", func(t *testing.T) {
		f := setupInventory(t)
		t.Setenv("DEVCTL_GITHUB_TOKEN", "")
		var out bytes.Buffer
		require.ErrorIs(t, execute(t.Context(), &out, "devctl"), authstore.ErrAuthRequired)
		require.Empty(t, out.String())
		require.Empty(t, f.paths)
	})
	t.Run("case 1: canceled command", func(t *testing.T) {
		setupInventory(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		var out bytes.Buffer
		require.ErrorIs(t, execute(ctx, &out, "devctl"), context.Canceled)
		require.Empty(t, out.String())
	})
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func Test_OwnerReturnsOutputErrors(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			setupInventory(t)
			failure := errors.New("output refused")
			require.ErrorIs(t, execute(t.Context(), failingWriter{failure}, "devctl", "-o", format), failure)
		})
	}
}
