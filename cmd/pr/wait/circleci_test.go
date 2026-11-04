package wait

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// publish only runs on tags; test cannot post until build has finished.
const circleConfig = `version: 2.1
workflows:
  ci:
    jobs:
      - build
      - test:
          requires: [build]
      - publish:
          filters:
            branches: {ignore: /.*/}
            tags: {only: /^v.*/}
`

func circleStatuses(testState string) sequence.Response {
	statuses := []any{map[string]any{"context": "ci/circleci: build", "state": "success", "target_url": "https://circleci.com/gh/o/r/1"}}
	if testState != "" {
		statuses = append(statuses, map[string]any{"context": "ci/circleci: test", "state": testState, "target_url": "https://circleci.com/gh/o/r/2"})
	}
	return sequence.Response{Body: map[string]any{"statuses": statuses}}
}

func circleRoutes(statuses ...sequence.Response) sequence.Routes {
	return sequence.Routes{
		"GET /repos/o/r/pulls/42": {{Body: map[string]any{
			"number": 42, "state": "open", "mergeable_state": "clean",
			"head": map[string]any{"sha": "abc123", "ref": "feature"}, "base": map[string]any{"ref": "main"},
		}}},
		"GET /repos/o/r/contents/.circleci/config.yml?ref=abc123": {{Body: map[string]any{
			"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(circleConfig)),
		}}},
		"GET /repos/o/r/commits/abc123/check-runs":    {{Body: map[string]any{"check_runs": []any{}}}},
		"GET /repos/o/r/commits/abc123/status":        statuses,
		"GET /repos/o/r/actions/runs?head_sha=abc123": {{Body: map[string]any{"workflow_runs": []any{}}}},
	}
}

func Test_run_circleCIFromStatuses(t *testing.T) {
	for _, tc := range []struct {
		name     string
		statuses []sequence.Response
		exit     int
		verdict  string
		reason   string
	}{
		{name: "waits for a downstream job", statuses: []sequence.Response{circleStatuses(""), circleStatuses("pending"), circleStatuses("success")}, verdict: "green"},
		{name: "missing downstream job", statuses: []sequence.Response{circleStatuses("")}, exit: agentcli.ExitTimeout, verdict: "timeout", reason: "ci/circleci: test (not posted yet)"},
		{name: "failed downstream job", statuses: []sequence.Response{circleStatuses("failure")}, exit: agentcli.ExitRed, verdict: "red", reason: "ci/circleci: test"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, stdout, stderr, server := newRunner(t, circleRoutes(tc.statuses...), loggedIn)
			cmd := &cobra.Command{}
			r.flag.Init(cmd)
			if err := cmd.ParseFlags([]string{"--circleci-from-statuses", "--progress", "--timeout=2m"}); err != nil {
				t.Fatal(err)
			}
			clock := agentcli.NewVirtualClock(time.Now())
			r.clock = func() (agentcli.Clock, error) { return clock, nil }
			// A nil token gate would panic if status mode asked for it.
			r.requireCircleCI = nil

			err := r.run(t.Context(), []string{"o/r", "42"})
			if agentcli.Exit(err) != tc.exit {
				t.Fatalf("want exit %d, got %v\n%s", tc.exit, err, stdout)
			}
			doc := decode(t, stdout)
			if doc["verdict"] != tc.verdict || !strings.Contains(doc["reason"].(string), tc.reason) {
				t.Fatalf("document: %v", doc)
			}
			if tc.verdict == "green" {
				reads := 0
				for _, req := range server.Requests() {
					if req.Path == "/repos/o/r/commits/abc123/status" {
						reads++
					}
				}
				if reads != 3 || !strings.Contains(stderr.String(), "ci/circleci: test (not posted yet)") {
					t.Errorf("green before all jobs posted and passed: %d status reads\n%s", reads, stderr)
				}
			}
		})
	}
}

func Test_run_circleCIAPIStillRequiresAuthentication(t *testing.T) {
	for _, args := range [][]string{nil, {"--circleci-from-statuses", "--failed-log"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			r, stdout, _, _ := newRunner(t, circleRoutes(circleStatuses("success")), loggedIn)
			cmd := &cobra.Command{}
			r.flag.Init(cmd)
			if err := cmd.ParseFlags(args); err != nil {
				t.Fatal(err)
			}
			err := r.run(t.Context(), []string{"o/r", "42"})
			if agentcli.Exit(err) != agentcli.ExitAuthRequired {
				t.Fatalf("want exit 8 for CircleCI API access, got %v", err)
			}
			if doc := decode(t, stdout); doc["verdict"] != "auth_required" || !strings.Contains(doc["reason"].(string), "devctl auth login --circleci-only") {
				t.Errorf("document: %v", doc)
			}
		})
	}
}
