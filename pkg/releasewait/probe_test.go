package releasewait_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
	"github.com/giantswarm/devctl/v8/pkg/releasewait/releasewaittest"
)

type probeFunc func(context.Context, releasewait.Artifact) (string, bool, error)

func (f probeFunc) Probe(ctx context.Context, a releasewait.Artifact) (string, bool, error) {
	return f(ctx, a)
}

// Exercise the public wait and the reporter used by the CLI, with no network,
// credentials or wall-clock sleeps. Two polls time out before the third failed
// probe would end the wait as a tooling error.
func TestWaitProbeDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name       string
		outcome    string
		polls      int
		wantCalls  int
		wantExit   int
		wantState  string
		wantError  string
		wantReason string
	}{
		{name: "retried error at timeout", outcome: "error", polls: 2, wantCalls: 2, wantExit: agentcli.ExitTimeout, wantState: releasewait.StateMissing, wantError: "chart read failed 2", wantReason: "missing; last probe error: chart read failed 2"},
		{name: "plain missing", outcome: "missing", polls: 2, wantCalls: 2, wantExit: agentcli.ExitTimeout, wantState: releasewait.StateMissing},
		{name: "answered missing clears error", outcome: "answered", polls: 2, wantCalls: 2, wantExit: agentcli.ExitTimeout, wantState: releasewait.StateMissing},
		{name: "available clears error", outcome: "available", polls: 2, wantCalls: 2, wantExit: agentcli.ExitOK, wantState: releasewait.StateAvailable},
		{name: "deadline during probe", outcome: "deadline", polls: 2, wantCalls: 2, wantExit: agentcli.ExitTimeout, wantState: releasewait.StateMissing, wantError: "chart read failed 2", wantReason: "missing; last probe error: chart read failed 2"},
		{name: "consecutive error limit", outcome: "error", polls: 4, wantCalls: 3, wantExit: agentcli.ExitUsage, wantState: releasewait.StateMissing, wantError: "chart read failed 3", wantReason: "failed 3 times in a row: chart read failed 3"},
		{name: "registry refusal", outcome: "refused", polls: 2, wantCalls: 1, wantExit: agentcli.ExitUsage, wantState: releasewait.StateMissing, wantError: "the registry answered HTTP 403 DENIED", wantReason: "the registry answered HTTP 403 DENIED"},
	} {
		for _, progress := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/progress=%t", tc.name, progress), func(t *testing.T) {
				sources, err := releasewaittest.GeneratedRelease("giantswarm", "fixture", &releasewaittest.Registry{}, "success")
				if err != nil {
					t.Fatal(err)
				}
				clock := agentcli.NewVirtualClock(time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC))
				timeout := time.Duration(tc.polls) * releasewait.IntervalFloor
				calls := 0
				prober := probeFunc(func(ctx context.Context, a releasewait.Artifact) (string, bool, error) {
					if a.Kind != releasewait.KindChart {
						return "sha256:image", true, nil
					}
					calls++
					switch {
					case tc.outcome == "missing" || tc.outcome == "answered" && calls > 1:
						return "", false, nil
					case tc.outcome == "available" && calls > 1:
						return "sha256:chart", true, nil
					case tc.outcome == "refused":
						return "", false, &releasewait.RegistryAnswerError{Reference: a.Reference, Status: 403, Code: "DENIED"}
					case tc.outcome == "deadline" && calls > 1:
						if err := clock.Sleep(ctx, timeout); err == nil {
							t.Fatal("probe did not reach the wait deadline")
						}
					}
					return "", false, fmt.Errorf("chart read failed %d", calls)
				})
				var stderr bytes.Buffer
				waiter, err := releasewait.New(releasewait.Config{
					Owner: "giantswarm", Repo: "fixture", Version: "v1.2.3", Timeout: timeout,
					GitHub: sources.GitHub, Entries: sources.Entries, CircleCI: sources.CircleCI,
					Registry: prober, Clock: clock, Progress: agentcli.NewProgress(&stderr, progress),
				})
				if err != nil {
					t.Fatal(err)
				}
				doc := releasewait.Document{Envelope: agentcli.NewEnvelope(releasewait.Command, clock.Now())}
				err = waiter.Wait(context.Background(), &doc.Result)
				if agentcli.Exit(err) != tc.wantExit || calls != tc.wantCalls {
					t.Fatalf("exit=%d, probes=%d, error=%v; want exit=%d, probes=%d", agentcli.Exit(err), calls, err, tc.wantExit, tc.wantCalls)
				}
				var stdout bytes.Buffer
				reportErr := agentcli.Report(&stdout, &doc, agentcli.VerdictAvailable, err)
				if agentcli.Exit(reportErr) != tc.wantExit {
					t.Fatalf("report exit=%d: %v", agentcli.Exit(reportErr), reportErr)
				}
				var printed struct {
					ExitCode  int              `json:"exitCode"`
					Reason    string           `json:"reason"`
					Artifacts []map[string]any `json:"artifacts"`
				}
				if err := json.Unmarshal(stdout.Bytes(), &printed); err != nil {
					t.Fatal(err)
				}
				if printed.ExitCode != tc.wantExit || !strings.Contains(printed.Reason, tc.wantReason) {
					t.Errorf("printed exit=%d, reason=%q; want exit=%d, reason containing %q", printed.ExitCode, printed.Reason, tc.wantExit, tc.wantReason)
				}
				if len(printed.Artifacts) != 2 {
					t.Fatalf("artifacts: %v", printed.Artifacts)
				}
				for _, a := range printed.Artifacts {
					wantError, wantState := "", releasewait.StateAvailable
					if a["kind"] == releasewait.KindChart {
						wantError, wantState = tc.wantError, tc.wantState
						if tc.wantExit == agentcli.ExitTimeout && !strings.Contains(printed.Reason, a["reference"].(string)) {
							t.Errorf("timeout reason %q does not name chart %v", printed.Reason, a)
						}
					}
					if a["state"] != wantState {
						t.Errorf("artifact state: %v; want %s", a, wantState)
					}
					if got, present := a["lastProbeError"]; wantError == "" && present || wantError != "" && got != wantError {
						t.Errorf("artifact diagnostic: %v; want lastProbeError=%q (omitted when empty)", a, wantError)
					}
				}
				if tc.wantError == "" && strings.Contains(printed.Reason, "last probe error") {
					t.Errorf("stale probe error in reason: %s", printed.Reason)
				}
			})
		}
	}
}
