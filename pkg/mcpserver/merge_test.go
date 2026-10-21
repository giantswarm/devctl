package mcpserver_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/mcpserver"
	"github.com/giantswarm/devctl/v8/pkg/mergejob"
	"github.com/giantswarm/devctl/v8/pkg/prmerge"
	"github.com/giantswarm/devctl/v8/pkg/prmerge/prmergetest"
	"github.com/giantswarm/devctl/v8/pkg/prwait/prwaittest"
	"github.com/giantswarm/devctl/v8/pkg/releasewait/releasewaittest"
)

// greenPR is giantswarm/devctl#42, opened by the caller, its head green,
// whose merge produces the commit the release of releasewaittest is cut
// from.
func greenPR() *prmergetest.GitHub {
	gh := prmergetest.New(prmergetest.PullRequest("giantswarm", "devctl", 42, "abc123", prmergetest.Login),
		prwaittest.Head{CheckRuns: []*github.CheckRun{prwaittest.CheckRun("test", "completed", "success")}})
	gh.MergeCommitSHA = releasewaittest.ReleaseSHA
	return gh
}

// mergeServer serves pr_merge over gh and the release of workflows, as
// person, its merges recorded in memory.
func mergeServer(t *testing.T, gh *prmergetest.GitHub, workflows ...string) (*sources, *mergejob.Memory, func(args map[string]any) map[string]any) {
	t.Helper()
	release, err := releasewaittest.GeneratedRelease("giantswarm", "devctl", &releasewaittest.Registry{}, workflows...)
	if err != nil {
		t.Fatal(err)
	}
	s := &sources{merge: gh, policy: prmergetest.Policy(false), release: release}
	merges := &mergejob.Memory{}
	c, _ := connect(t, mcpserver.Config{Credentials: person("ghu_person"), Sources: s, Clock: virtual(), Merges: merges})
	return s, merges, func(args map[string]any) map[string]any {
		t.Helper()
		base := map[string]any{"repository": "giantswarm/devctl", "number": 42}
		for k, v := range args {
			base[k] = v
		}
		_, doc := call(t, c, mcpserver.ToolPRMerge, base, "merge-1")
		return doc
	}
}

// TestPRMergeTwiceMergesOnce: a pull request merged between two calls gets
// one merge API request and two merged verdicts, its release in both.
func TestPRMergeTwiceMergesOnce(t *testing.T) {
	gh := greenPR()
	s, merges, merge := mergeServer(t, gh, "success")

	first, second := merge(nil), merge(nil)
	for i, doc := range []map[string]any{first, second} {
		assertVerdict(t, doc, agentcli.VerdictGreen, agentcli.ExitOK)
		if doc["command"] != prmerge.Command || doc["mergeCommitSha"] != releasewaittest.ReleaseSHA || doc["mergedBy"] != prmergetest.Login {
			t.Errorf("call %d: command %v, mergeCommitSha %v, mergedBy %v", i+1, doc["command"], doc["mergeCommitSha"], doc["mergedBy"])
		}
		release, _ := doc["release"].(map[string]any)
		if release["verdict"] != string(agentcli.VerdictAvailable) || release["tag"] != "v1.2.3" {
			t.Errorf("call %d: release %v, want v1.2.3 available", i+1, release)
		}
	}
	if n := len(gh.Merges()); n != 1 {
		t.Errorf("%d merge API requests, want 1", n)
	}
	if got := gh.Merges()[0].HeadSHA; got != "abc123" {
		t.Errorf("the merge expected head %q, want the judged abc123", got)
	}
	if !hasWarning(second, "was merged already") || hasWarning(first, "was merged already") {
		t.Errorf("warnings: first %v, second %v; want the second alone to say it was merged already", first["warnings"], second["warnings"])
	}
	if !slices.Equal(s.tokens, []string{"ghu_person", "ghu_person", "ghu_person", "ghu_person"}) {
		t.Errorf("the clients were built with %v, want the person's token for each merge and release", s.tokens)
	}
	jobs, _ := merges.List()
	if len(jobs) != 2 {
		t.Fatalf("pr_merge_status lists %d merges, want 2", len(jobs))
	}
	for _, job := range jobs {
		if job.State != mergejob.StateFinished || job.Repository != "giantswarm/devctl" || job.Number != 42 || len(job.Document) == 0 {
			t.Errorf("merge record %+v, want a finished giantswarm/devctl#42 with its document", job.Job)
		}
	}
}

// TestPRMergeReleaseOutcomesStayDistinct: after the merge, a release that
// failed is exit 6 and one not confirmed in time exit 9, on the merging call
// and on a second call alike.
func TestPRMergeReleaseOutcomesStayDistinct(t *testing.T) {
	tests := []struct {
		name      string
		workflows []string
		verdict   agentcli.Verdict
		exit      int
	}{
		{name: "the release failed", workflows: []string{"failed"}, verdict: agentcli.VerdictReleaseFailed, exit: agentcli.ExitReleaseFailed},
		{name: "the release not confirmed", workflows: []string{"running"}, verdict: agentcli.VerdictReleaseUnconfirmed, exit: agentcli.ExitReleaseUnconfirmed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gh := greenPR()
			_, _, merge := mergeServer(t, gh, tt.workflows...)
			for i := 1; i <= 2; i++ {
				doc := merge(map[string]any{"releaseTimeout": "10m"})
				assertVerdict(t, doc, tt.verdict, tt.exit)
				if doc["mergeCommitSha"] != releasewaittest.ReleaseSHA {
					t.Errorf("call %d: mergeCommitSha %v, want the merge's", i, doc["mergeCommitSha"])
				}
			}
			if n := len(gh.Merges()); n != 1 {
				t.Errorf("%d merge API requests, want 1", n)
			}
		})
	}
}

// TestPRMergeRefusesWithoutProgressToken: the merge is a wait too and is
// never started without the progress that keeps its call alive.
func TestPRMergeRefusesWithoutProgressToken(t *testing.T) {
	gh := greenPR()
	s := &sources{merge: gh, policy: prmergetest.Policy(false)}
	c, _ := connect(t, mcpserver.Config{Credentials: person("ghu_person"), Sources: s, Clock: virtual()})
	_, doc := call(t, c, mcpserver.ToolPRMerge, map[string]any{"repository": "giantswarm/devctl", "number": 42}, nil)
	assertVerdict(t, doc, agentcli.VerdictProgressTokenRequired, agentcli.ExitUsage)
	if len(gh.Merges()) != 0 || len(s.tokens) != 0 {
		t.Errorf("a refused call merged %d times and built clients with %v", len(gh.Merges()), s.tokens)
	}
}

func hasWarning(doc map[string]any, substring string) bool {
	warnings, _ := doc["warnings"].([]any)
	for _, w := range warnings {
		if s, _ := w.(string); strings.Contains(s, substring) {
			return true
		}
	}
	return false
}
