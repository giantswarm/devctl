package wait

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

// runCommand runs the command with clients that refuse to open, so what is
// tested is the argument handling, the gate and the envelope.
func runCommand(t *testing.T, args []string, f *flag, openErr error) (map[string]any, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	r := &runner{
		flag:   f,
		stdout: &stdout,
		stderr: &stderr,
		open: func(context.Context, agentcli.Endpoints, http.RoundTripper, func(string)) (*clients, error) {
			return nil, openErr
		},
	}
	err := r.run(context.Background(), args)
	var doc map[string]any
	if jsonErr := json.Unmarshal(stdout.Bytes(), &doc); jsonErr != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", jsonErr, stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr should stay empty without --progress, got %q", stderr.String())
	}
	return doc, err
}

func TestRunUsageErrorsAreDocuments(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		flag   flag
		reason string
	}{
		{name: "no slash", args: []string{"devctl", "v1.2.3"}, reason: "expected owner/repo"},
		{name: "neither version nor pr", args: []string{"giantswarm/devctl"}, reason: "exactly one of a version"},
		{name: "both version and pr", args: []string{"giantswarm/devctl", "v1.2.3"}, flag: flag{PR: 7}, reason: "exactly one of a version"},
		{name: "not a version", args: []string{"giantswarm/devctl", "latest"}, reason: "not a version"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.flag
			doc, err := runCommand(t, tc.args, &f, errors.New("must not be opened"))
			if agentcli.Exit(err) != agentcli.ExitUsage {
				t.Fatalf("want exit %d, got %d (%v)", agentcli.ExitUsage, agentcli.Exit(err), err)
			}
			if doc["command"] != "release wait" || doc["verdict"] != string(agentcli.VerdictUsage) || doc["exitCode"] != float64(agentcli.ExitUsage) {
				t.Errorf("envelope: %v", doc)
			}
			if reason, _ := doc["reason"].(string); !strings.Contains(reason, tc.reason) {
				t.Errorf("reason %q does not contain %q", reason, tc.reason)
			}
			if doc["repository"] == nil || doc["artifacts"] == nil || doc["actions"] == nil {
				t.Errorf("the document should carry the result fields: %v", doc)
			}
		})
	}
}

func TestRunAuthRequiredIsExit8(t *testing.T) {
	authErr := &authstore.AuthRequiredError{Identity: "GitHub", Cause: "no token in the keychain", Hint: "devctl auth login"}
	doc, err := runCommand(t, []string{"giantswarm/devctl", "v1.2.3"}, &flag{}, authErr)
	if agentcli.Exit(err) != agentcli.ExitAuthRequired {
		t.Fatalf("want exit %d, got %d (%v)", agentcli.ExitAuthRequired, agentcli.Exit(err), err)
	}
	if doc["verdict"] != string(agentcli.VerdictAuthRequired) || !strings.Contains(doc["reason"].(string), "devctl auth login") {
		t.Errorf("envelope: %v", doc)
	}
}
