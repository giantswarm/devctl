package prwait

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// ciRun is the head's listing of one GitHub Actions run, CI.
func ciRun(status, conclusion string) sequence.Response {
	return body(map[string]any{"total_count": 1, "workflow_runs": []any{
		map[string]any{"id": 7, "name": "CI", "status": status, "conclusion": conclusion, "html_url": "https://github.com/o/r/actions/runs/7"},
	}})
}

// otherAppRun is a check run of another app than GitHub Actions the base
// does not require, such as a plan an infrastructure app posts.
func otherAppRun(status, conclusion string) map[string]any {
	r := run("Tofu Plan", status, conclusion)
	r["id"] = 8
	r["app"] = map[string]any{"slug": "tofu-controller"}
	return r
}

// runID is run with its own id, so two check runs of a head are told apart.
func runID(id int, name, status, conclusion string) map[string]any {
	r := run(name, status, conclusion)
	r["id"] = id
	return r
}

// pollLines matches the progress line of each poll.
var pollLines = regexp.MustCompile(`(?m)^poll \d+: `)

// The shapes of one race: a verdict judged on what a head pushed seconds
// earlier has not registered yet, or on a required context that is still
// running. Polls are MinInterval apart on the virtual clock, so a head
// updated at the start passes RegistrationGrace at poll 5.
func Test_Wait_registration(t *testing.T) {
	young := func() sequence.Routes { return emptyHead(time.Now(), "fix", "") }
	old := func() sequence.Routes { return emptyHead(longAgo, "fix", "") }
	for _, tc := range []struct {
		name  string
		gh    func() sequence.Routes
		exit  int
		v     agentcli.Verdict
		polls int
		// progress and reason are substrings of the progress lines and of
		// the verdict's reason.
		progress string
		reason   string
	}{
		{
			// The required context's workflow registers after the first
			// poll: not required_missing, the wait goes on to green.
			name: "required context not registered yet",
			gh: func() sequence.Routes {
				gh := young()
				requiresLint(gh)
				gh[runsPath] = []sequence.Response{checkRuns(), checkRuns(run("lint", "in_progress", "")), checkRuns(run("lint", "completed", "success"))}
				gh[actsPath] = []sequence.Response{noRuns, ciRun("in_progress", ""), ciRun("completed", "success")}
				return gh
			},
			exit: agentcli.ExitOK, v: agentcli.VerdictGreen, polls: 3,
			progress: "poll 1: waiting for required context lint (absent)",
		},
		{
			// No required context and no workflow the head is known to run
			// (a paths filter): nothing registered yet is not green.
			name: "runs not registered yet, nothing required",
			gh: func() sequence.Routes {
				gh := emptyHead(time.Now(), "fix", "  pull_request:\n    paths: ['go/**']")
				gh[runsPath] = []sequence.Response{checkRuns(), checkRuns(run("test", "in_progress", "")), checkRuns(run("test", "completed", "success"))}
				gh[actsPath] = []sequence.Response{noRuns, ciRun("in_progress", ""), ciRun("completed", "success")}
				return gh
			},
			exit: agentcli.ExitOK, v: agentcli.VerdictGreen, polls: 3,
			progress: "poll 1: waiting for actions runs of the head (none registered",
		},
		{
			// Another app reported and finished before the workflows
			// registered: its check run is no registration of the head's runs.
			name: "another app finished before the runs registered",
			gh: func() sequence.Routes {
				gh := young()
				gh[runsPath] = []sequence.Response{
					checkRuns(otherAppRun("completed", "success")),
					checkRuns(otherAppRun("completed", "success"), run("test", "in_progress", "")),
					checkRuns(otherAppRun("completed", "success"), run("test", "completed", "success")),
				}
				gh[actsPath] = []sequence.Response{noRuns, ciRun("in_progress", ""), ciRun("completed", "success")}
				return gh
			},
			exit: agentcli.ExitOK, v: agentcli.VerdictGreen, polls: 3,
			progress: "poll 1: waiting for actions runs of the head (none registered",
		},
		{
			// --update-branch replaced a settled head with the merge of the
			// base: the new head is judged on its own runs, not required_missing
			// before they register.
			name: "the head --update-branch creates",
			gh: func() sequence.Routes {
				gh := old()
				requiresLint(gh)
				updated := pull("clean", map[string]any{
					"updated_at": time.Now().UTC().Format(time.RFC3339Nano),
					"head":       map[string]any{"sha": "def456", "ref": "feature", "repo": map[string]any{"full_name": "o/r"}},
				})
				gh[pullPath] = []sequence.Response{gh[pullPath][0], updated}
				gh[runsPath] = []sequence.Response{checkRuns(run("lint", "in_progress", ""))}
				gh["GET /repos/o/r/commits/def456/check-runs"] = []sequence.Response{checkRuns(), checkRuns(run("lint", "in_progress", "")), checkRuns(run("lint", "completed", "success"))}
				gh["GET /repos/o/r/commits/def456/status"] = []sequence.Response{noStatuses}
				gh["GET /repos/o/r/actions/runs?head_sha=def456"] = []sequence.Response{noRuns, ciRun("in_progress", ""), ciRun("completed", "success")}
				return gh
			},
			exit: agentcli.ExitOK, v: agentcli.VerdictGreen, polls: 4,
			progress: "poll 2: waiting for required context lint (absent)",
		},
		{
			// The required run is registered and running beside another
			// app's pending checks: the wait goes on to its result.
			name: "required run registered and running",
			gh: func() sequence.Routes {
				gh := old()
				requiresLint(gh)
				gh[runsPath] = []sequence.Response{
					checkRuns(otherAppRun("in_progress", ""), run("lint", "in_progress", "")),
					checkRuns(otherAppRun("in_progress", ""), run("lint", "completed", "success")),
				}
				gh[actsPath] = []sequence.Response{ciRun("in_progress", ""), ciRun("completed", "success")}
				return gh
			},
			exit: agentcli.ExitOK, v: agentcli.VerdictGreen, polls: 2,
			progress: "poll 1: waiting for check lint (in_progress)",
		},
		{
			// A required context is absent while another app's check is
			// still pending: that check may be what reports it, so the head is
			// not settled until it finishes.
			name: "required context absent while another app runs",
			gh: func() sequence.Routes {
				gh := old()
				requiresLint(gh)
				gh[runsPath] = []sequence.Response{checkRuns(otherAppRun("in_progress", "")), checkRuns(otherAppRun("completed", "success"))}
				return gh
			},
			exit: agentcli.ExitRequiredMissing, v: agentcli.VerdictRequiredMissing, polls: 2,
			reason: "required context(s) never reported: lint; every check, run and workflow of the head has finished",
		},
		{
			// A check the base does not require failed while the required
			// one runs: the wait waits for the required result, then is red.
			name: "non-required red waits for the required run",
			gh: func() sequence.Routes {
				gh := old()
				requiresLint(gh)
				gh[runsPath] = []sequence.Response{
					checkRuns(runID(2, "docs", "completed", "failure"), run("lint", "in_progress", "")),
					checkRuns(runID(2, "docs", "completed", "failure"), run("lint", "completed", "success")),
				}
				return gh
			},
			exit: agentcli.ExitRed, v: agentcli.VerdictRed, polls: 2,
			reason: "check docs concluded failure",
		},
		{
			name: "required red is red at once",
			gh: func() sequence.Routes {
				gh := old()
				requiresLint(gh)
				gh[runsPath] = []sequence.Response{checkRuns(run("lint", "completed", "failure"), runID(2, "docs", "in_progress", ""))}
				return gh
			},
			exit: agentcli.ExitRed, v: agentcli.VerdictRed, polls: 1,
			reason: "check lint concluded failure",
		},
		{
			// Past the grace a required context that never reports is exit 4
			// with the wording it always had.
			name: "required context never reports past the grace",
			gh: func() sequence.Routes {
				gh := young()
				requiresLint(gh)
				return gh
			},
			exit: agentcli.ExitRequiredMissing, v: agentcli.VerdictRequiredMissing, polls: 5,
			reason: "required context(s) never reported: lint; every check, run and workflow of the head has finished",
		},
		{
			name: "required context never reported on an old head",
			gh: func() sequence.Routes {
				gh := old()
				requiresLint(gh)
				return gh
			},
			exit: agentcli.ExitRequiredMissing, v: agentcli.VerdictRequiredMissing, polls: 1,
			reason: "required context(s) never reported: lint",
		},
		{
			// A repository without CI: a young head that registers nothing
			// is green once the grace has passed.
			name: "nothing registers within the grace",
			gh:   young,
			exit: agentcli.ExitOK, v: agentcli.VerdictGreen, polls: 5,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := start(t, tc.gh(), sequence.Routes{}, false, 30*time.Minute)
			_, err := h.waiter.Wait(context.Background(), "o", "r", 42)
			got := agentcli.VerdictGreen
			if err != nil {
				got = verdict(err)
			}
			if exitCode(err) != tc.exit || got != tc.v {
				t.Fatalf("want exit %d %s, got %d %s %v\n%s", tc.exit, tc.v, exitCode(err), got, err, h.progress.String())
			}
			if tc.reason != "" && !strings.Contains(err.Error(), tc.reason) {
				t.Errorf("reason: want %q in %q", tc.reason, err.Error())
			}
			if tc.progress != "" && !strings.Contains(h.progress.String(), tc.progress) {
				t.Errorf("progress: want %q in\n%s", tc.progress, h.progress.String())
			}
			if polls := len(pollLines.FindAllString(h.progress.String(), -1)); polls != tc.polls {
				t.Errorf("want %d poll(s), got %d:\n%s", tc.polls, polls, h.progress.String())
			}
		})
	}
}
