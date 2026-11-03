package job

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/spf13/cobra"

	circlemock "github.com/giantswarm/devctl/v8/e2e/mock/circleci"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/ci"
)

func loggedIn(context.Context) (authstore.Token, error) {
	return authstore.Token{Value: "token_test", Login: "someone"}, nil
}

var job7 = sequence.Routes{
	"GET /api/v2/project/gh/o/r/job/7":   {{Body: map[string]any{"number": 7, "name": "push-go", "status": "success", "web_url": "https://circleci.com/gh/o/r/7"}}},
	"GET /api/v1.1/project/github/o/r/7": {{Body: map[string]any{"build_num": 7, "steps": []any{}}}},
}

func start(t *testing.T, cc sequence.Routes) (*runner, *circlemock.Server) {
	t.Helper()
	ccServer, err := circlemock.Start(cc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ccServer.Close)
	endpoints := agentcli.DefaultEndpoints()
	endpoints.CircleCIAPIURL = ccServer.APIURL()
	return &runner{
		flag:            &flag{Lines: ci.DefaultJobLogLines},
		gate:            func(bool) error { return nil },
		stdout:          &bytes.Buffer{},
		requireCircleCI: loggedIn,
		endpoints:       func() agentcli.Endpoints { return endpoints },
		clock:           agentcli.SystemClock,
	}, ccServer
}

func execute(t *testing.T, r *runner, args ...string) (map[string]any, int) {
	t.Helper()
	r.stdout.(*bytes.Buffer).Reset()
	err := r.Run(&cobra.Command{}, args)
	var doc map[string]any
	if err := json.Unmarshal(r.stdout.(*bytes.Buffer).Bytes(), &doc); err != nil {
		t.Fatalf("document: %v", err)
	}
	if int(doc["exitCode"].(float64)) != agentcli.Exit(err) {
		t.Errorf("exitCode %v, process exit %d", doc["exitCode"], agentcli.Exit(err))
	}
	return doc, agentcli.Exit(err)
}

// TestReadsTheJob: the job is read with the keychain login and reported as
// listed.
func TestReadsTheJob(t *testing.T) {
	r, cc := start(t, job7)
	doc, code := execute(t, r, "o/r", "7")
	if code != agentcli.ExitOK || doc["verdict"] != "listed" || doc["command"] != "ci job" || doc["repository"] != "o/r" {
		t.Fatalf("exit %d: %v", code, doc)
	}
	if doc["job"].(map[string]any)["name"] != "push-go" {
		t.Errorf("job: %v", doc["job"])
	}
	for _, req := range cc.Requests() {
		if req.Header.Get("Circle-Token") != "token_test" {
			t.Errorf("a request without the login's token: %v", req)
		}
	}
}

// TestNoCircleCILogin: no CircleCI token is exit 8 before any CircleCI call.
func TestNoCircleCILogin(t *testing.T) {
	r, cc := start(t, job7)
	r.requireCircleCI = func(context.Context) (authstore.Token, error) {
		return authstore.Token{}, &authstore.AuthRequiredError{Identity: "CircleCI", Cause: "no token in the keychain", Hint: "devctl auth login --circleci-only"}
	}
	if doc, code := execute(t, r, "o/r", "7"); code != agentcli.ExitAuthRequired {
		t.Fatalf("exit %d: %v", code, doc["reason"])
	}
	if len(cc.Requests()) != 0 {
		t.Errorf("CircleCI was called: %v", cc.Requests())
	}
}

// TestUsage: a wrong call is exit 7 with a document, before any CircleCI
// call.
func TestUsage(t *testing.T) {
	r, cc := start(t, sequence.Routes{})
	if doc, code := execute(t, r, "o/r"); code != agentcli.ExitUsage || doc["command"] != "ci job" {
		t.Fatalf("exit %d: %v", code, doc)
	}
	r.flag.Lines = 0
	if doc, code := execute(t, r, "o/r", "7"); code != agentcli.ExitUsage || doc["verdict"] != "usage" {
		t.Fatalf("--lines 0: exit %d: %v", code, doc)
	}
	if len(cc.Requests()) != 0 {
		t.Errorf("CircleCI was called: %v", cc.Requests())
	}
}
