package update

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/updater"
)

type installOutcome struct {
	*updater.Updater
	result updater.InstallResult
	err    error
	calls  int
}

func (s *installOutcome) InstallLatestWithResult() (updater.InstallResult, error) {
	s.calls++
	return s.result, s.err
}

func TestRunReportsTheActualInstallOutcome(t *testing.T) {
	installErr := errors.New("signature verification refused the binary")
	for _, tc := range []struct {
		name       string
		discovered string
		result     updater.InstallResult
		installErr error
		want       string
		calls      int
	}{
		{"withdrawn", "2.0.0", updater.InstallResult{Version: "1.0.0"}, nil, "installed binary is unchanged", 1},
		{"changed newer", "2.0.0", updater.InstallResult{Version: "3.0.0", Installed: true}, nil, "Updated successfully to 3.0.0.", 1},
		{"installed", "2.0.0", updater.InstallResult{Version: "2.0.0", Installed: true}, nil, "Updated successfully to 2.0.0.", 1},
		{"refused", "2.0.0", updater.InstallResult{}, installErr, "Fetching latest built binary", 1},
		{"already current", "1.0.0", updater.InstallResult{}, nil, "You are already using the latest version.", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			binary := "devctl-" + runtime.GOOS + "-" + runtime.GOARCH
			if runtime.GOOS == "windows" {
				binary += ".exe"
			}
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/giantswarm/devctl/releases":
					_ = json.NewEncoder(w).Encode([]map[string]any{{
						"id": 2, "tag_name": "v" + tc.discovered,
						"assets": []map[string]any{{"id": 20, "name": binary, "browser_download_url": "https://example.test/" + binary}},
					}})
				case "/repos/giantswarm/devctl/releases/latest":
					w.WriteHeader(http.StatusNotFound)
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			t.Cleanup(api.Close)
			u, err := updater.New(updater.Config{
				CurrentVersion: "1.0.0", RepositoryURL: "https://github.com/giantswarm/devctl", GitHubAPIURL: api.URL,
			})
			if err != nil {
				t.Fatal(err)
			}
			s := &installOutcome{Updater: u, result: tc.result, err: tc.installErr}
			var stdout, stderr bytes.Buffer
			r := &runner{
				flag: &flag{NoCache: true}, stdout: &stdout, stderr: &stderr,
				newUpdater: func(noCache bool, warnings io.Writer) (updateService, error) {
					if !noCache || warnings != &stderr {
						t.Error("updater factory lost no-cache or stderr")
					}
					return s, nil
				},
			}
			err = r.run(context.Background(), &cobra.Command{}, nil)
			if !errors.Is(err, tc.installErr) {
				t.Fatalf("run = %v, want %v", err, tc.installErr)
			}
			if !strings.Contains(stdout.String(), tc.want) || s.calls != tc.calls {
				t.Fatalf("output = %q, install calls %d; want %q and %d", stdout.String(), s.calls, tc.want, tc.calls)
			}
			if !tc.result.Installed && strings.Contains(stdout.String(), "Updated successfully") {
				t.Fatalf("reported success without installation: %q", stdout.String())
			}
		})
	}
}
