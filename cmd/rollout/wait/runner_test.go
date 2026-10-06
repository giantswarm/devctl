package wait

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"k8s.io/client-go/dynamic"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
	"github.com/giantswarm/devctl/v8/pkg/rolloutwait"
)

// runCommand runs the command with release sources that refuse to open, so
// what is tested is the argument handling, the gate and the envelope.
func runCommand(t *testing.T, args []string, f *flag, openErr error) (map[string]any, error) {
	t.Helper()
	return runCommandWithCluster(t, args, f, openErr, nil)
}

// runCommandWithCluster is runCommand with the cluster opening failing with
// clusterErr (opening it is a no-op when nil).
func runCommandWithCluster(t *testing.T, args []string, f *flag, openErr, clusterErr error) (map[string]any, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	r := &runner{
		gate:   func(bool) error { return nil },
		flag:   f,
		stdout: &stdout,
		stderr: &stderr,
		openRelease: func(context.Context, agentcli.Endpoints, http.RoundTripper, func(string)) (*releasewait.Sources, error) {
			if clusterErr != nil {
				t.Error("the release must not be read when the kube context fails")
			}
			return nil, openErr
		},
		openCluster: func(string, string, func(http.RoundTripper) http.RoundTripper) (dynamic.Interface, error) {
			return nil, clusterErr
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
		{name: "no arguments", args: nil, reason: "usage: devctl rollout wait <installation> <owner/repo>"},
		{name: "no repository", args: []string{"myinstallation"}, reason: "got 1 argument(s)"},
		{name: "too many arguments", args: []string{"myinstallation", "giantswarm/devctl", "v1.2.3", "v1.2.4"}, reason: "got 4 argument(s)"},
		{name: "installation with a slash", args: []string{"giantswarm/devctl", "v1.2.3"}, reason: "is not an installation name"},
		{name: "no slash", args: []string{"myinstallation", "devctl", "v1.2.3"}, reason: "expected owner/repo"},
		{name: "neither version nor pr", args: []string{"myinstallation", "giantswarm/devctl"}, reason: "exactly one of a version"},
		{name: "both version and pr", args: []string{"myinstallation", "giantswarm/devctl", "v1.2.3"}, flag: flag{PR: 7}, reason: "exactly one of a version"},
		{name: "not a version", args: []string{"myinstallation", "giantswarm/devctl", "latest"}, reason: "not a version"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.flag
			doc, err := runCommand(t, tc.args, &f, errors.New("must not be opened"))
			if agentcli.Exit(err) != agentcli.ExitUsage {
				t.Fatalf("want exit %d, got %d (%v)", agentcli.ExitUsage, agentcli.Exit(err), err)
			}
			if doc["command"] != "rollout wait" || doc["verdict"] != string(agentcli.VerdictUsage) || doc["exitCode"] != float64(agentcli.ExitUsage) {
				t.Errorf("envelope: %v", doc)
			}
			if reason, _ := doc["reason"].(string); !strings.Contains(reason, tc.reason) {
				t.Errorf("reason %q does not contain %q", reason, tc.reason)
			}
			if doc["release"] == nil || doc["deployments"] == nil || doc["charts"] == nil {
				t.Errorf("the document should carry the result fields: %v", doc)
			}
		})
	}
}

func TestRunNamesTheTeleportContext(t *testing.T) {
	doc, _ := runCommand(t, []string{"myinstallation", "giantswarm/devctl"}, &flag{}, nil)
	if doc["installation"] != "myinstallation" || doc["context"] != "teleport.giantswarm.io-myinstallation" {
		t.Errorf("document: %v", doc)
	}
	doc, _ = runCommand(t, []string{"myinstallation", "giantswarm/devctl"}, &flag{Context: "kind-lab"}, nil)
	if doc["context"] != "kind-lab" {
		t.Errorf("--context: %v", doc)
	}
}

func TestRunAuthRequiredIsExit8(t *testing.T) {
	authErr := &authstore.AuthRequiredError{Identity: "GitHub", Cause: "no token in the keychain", Hint: "devctl auth login"}
	doc, err := runCommand(t, []string{"myinstallation", "giantswarm/devctl", "v1.2.3"}, &flag{}, authErr)
	if agentcli.Exit(err) != agentcli.ExitAuthRequired {
		t.Fatalf("want exit %d, got %d (%v)", agentcli.ExitAuthRequired, agentcli.Exit(err), err)
	}
	if doc["verdict"] != string(agentcli.VerdictAuthRequired) || !strings.Contains(doc["reason"].(string), "devctl auth login") {
		t.Errorf("envelope: %v", doc)
	}
}

func TestRunMissingContextEndsBeforeTheRelease(t *testing.T) {
	missing := rolloutwait.MissingContextError("myinstallation", "teleport.giantswarm.io-myinstallation", []string{"gs-myinstallation"})
	doc, err := runCommandWithCluster(t, []string{"myinstallation", "giantswarm/devctl", "v1.2.3"}, &flag{}, nil, missing)
	if agentcli.Exit(err) != agentcli.ExitUsage {
		t.Fatalf("want exit %d, got %d (%v)", agentcli.ExitUsage, agentcli.Exit(err), err)
	}
	if reason, _ := doc["reason"].(string); !strings.Contains(reason, "gs-myinstallation") || !strings.Contains(reason, "--context") {
		t.Errorf("reason should name the candidate and --context: %q", reason)
	}
}
