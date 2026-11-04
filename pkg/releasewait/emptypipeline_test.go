package releasewait

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

const emptyPipelineConfig = `version: 2.1
workflows:
  publish:
    jobs:
    - publish:
        filters:
          tags:
            only: /^v[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$/
          branches:
            ignore: /.*/
`

// emptyPipelinePolls counts actual workflow listings, including those made
// by the real HTTP client after setup or continuation finishes.
type emptyPipelinePolls struct {
	CircleCI
	polls int
}

func (c *emptyPipelinePolls) ListPipelineWorkflows(ctx context.Context, id string) ([]circleciclient.Workflow, error) {
	c.polls++
	return c.CircleCI.ListPipelineWorkflows(ctx, id)
}

func emptyPipelineFixture(t *testing.T, state string, generated bool) fixture {
	t.Helper()
	circleFiles := []string{"config.yml"}
	config := emptyPipelineConfig
	if generated {
		circleFiles = append(circleFiles, "workflows.yml")
		config = "setup: true\n# .circleci/workflows.yml\n" + config
	}
	gh := baseGitHub([]string{"README.md"}, []string{"zz_generated.auto_release.yaml"}, circleFiles)
	// A read at any other revision does not match: the tag's configuration
	// is the evidence, even when main now carries different filters.
	gh[repoRoute("/contents/.circleci/config.yml?ref="+testSHA)] = []sequence.Response{{Body: fileContent(".circleci/config.yml", config)}}
	if generated {
		gh[repoRoute("/contents/.circleci/workflows.yml?ref="+testSHA)] = []sequence.Response{{Body: fileContent(".circleci/workflows.yml", emptyPipelineConfig)}}
	}
	cc := pipelineRoutes([][]map[string]any{{}}, nil)
	cc["GET /api/v2/project/gh/giantswarm/kserve/pipeline"] = []sequence.Response{{Body: map[string]any{
		"items": []map[string]any{{"id": "p1", "number": 12, "state": state, "vcs": map[string]any{"tag": testTag, "revision": testSHA}}},
	}}}
	fx := fixture{github: gh, circleci: cc}
	if generated {
		fx.entry = generatedEntry(t)
		fx.entry.Gen.Flavours = []string{"cli"}
	}
	return fx
}

func TestWaitEmptyTagPipelineEndsWithinOnePoll(t *testing.T) {
	for _, state := range []string{"created", "errored"} {
		for _, generated := range []bool{false, true} {
			for _, pr := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/generated=%t/pr=%t", state, generated, pr), func(t *testing.T) {
					fx := emptyPipelineFixture(t, state, generated)
					tag := testTag
					if pr {
						tag += "-rc.2"
						fx.pr, fx.mergeCommit = 172, testSHA
						fx.github[repoRoute("/tags")] = []sequence.Response{{Body: []map[string]any{{"name": tag, "commit": map[string]any{"sha": testSHA}}}}}
						fx.circleci["GET /api/v2/project/gh/giantswarm/kserve/pipeline"] = []sequence.Response{{Body: map[string]any{
							"items": []map[string]any{{"id": "p1", "number": 12, "state": state, "vcs": map[string]any{"tag": tag, "revision": testSHA}}},
						}}}
					}
					clock := agentcli.NewVirtualClock(time.Now())
					start := clock.Now()
					fx.clock, fx.timeout = &clock, 30*time.Minute
					var counter *emptyPipelinePolls
					fx.wrapCircleCI = func(c CircleCI) CircleCI {
						counter = &emptyPipelinePolls{CircleCI: c}
						return counter
					}
					result, err := run(t, fx)
					assertExit(t, err, agentcli.ExitTagPipelineEmpty, "re-trigger the pipeline for tag "+tag)
					if counter.polls != 1 || !clock.Now().Equal(start) {
						t.Fatalf("wait used %d polls and %s", counter.polls, clock.Now().Sub(start))
					}
					if result.Tag != tag || result.SHA != testSHA || result.Pipeline == nil {
						t.Fatalf("missing tag/pipeline evidence: %+v", result)
					}
					pipelineURL := circleciclient.PipelineURL(testOwner, testRepo, 12)
					if result.Pipeline.ID != "p1" || result.Pipeline.Number != 12 || result.Pipeline.URL != pipelineURL || len(result.Pipeline.Workflows) != 0 {
						t.Fatalf("pipeline evidence: %+v", result.Pipeline)
					}
					for _, reason := range []string{"pipeline 12", pipelineURL, state + " with no workflows"} {
						if !strings.Contains(err.Error(), reason) {
							t.Errorf("reason %q does not contain %q", err, reason)
						}
					}
					var stdout bytes.Buffer
					doc := Document{Envelope: agentcli.NewEnvelope(Command, start), Result: result}
					reportErr := agentcli.Report(&stdout, &doc, agentcli.VerdictAvailable, err)
					var decoded Document
					if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
						t.Fatal(err)
					}
					if agentcli.Exit(reportErr) != agentcli.ExitTagPipelineEmpty || decoded.ExitCode != agentcli.ExitTagPipelineEmpty || decoded.Verdict != agentcli.VerdictTagPipelineEmpty || decoded.Pipeline == nil || decoded.Pipeline.URL != pipelineURL {
						t.Fatalf("document lost the outcome: %+v (%v)", decoded, reportErr)
					}
				})
			}
		}
	}
}

func TestWaitEmptyTagPipelineKeepsWaitingForWorkflows(t *testing.T) {
	for _, state := range []string{"setup-pending", "setup", "pending", "", "unknown"} {
		t.Run(state, func(t *testing.T) {
			fx := emptyPipelineFixture(t, state, false)
			fx.circleci["GET /api/v2/pipeline/p1/workflow"] = []sequence.Response{
				{Body: map[string]any{"items": []any{}}},
				{Body: map[string]any{"items": []any{wf("w1", "publish", "success", "2026-10-06T10:00:00Z")}}},
			}
			fx.circleci["GET /api/v2/workflow/w1/job"] = []sequence.Response{{Body: map[string]any{"items": []any{job("publish", "success")}}}}
			fx.github[repoRoute("/releases/tags/"+testTag)] = []sequence.Response{{Body: map[string]any{"tag_name": testTag, "draft": false, "published_at": "2026-10-06T10:00:00Z"}}}
			clock := agentcli.NewVirtualClock(time.Now())
			start := clock.Now()
			fx.clock = &clock
			var counter *emptyPipelinePolls
			fx.wrapCircleCI = func(c CircleCI) CircleCI {
				counter = &emptyPipelinePolls{CircleCI: c}
				return counter
			}
			result, err := run(t, fx)
			assertExit(t, err, agentcli.ExitOK, "")
			if counter.polls != 2 || !clock.Now().After(start) || result.Pipeline == nil || len(result.Pipeline.Workflows) != 1 {
				t.Fatalf("want the workflows from the second poll: %d polls, %s, %+v", counter.polls, clock.Now().Sub(start), result.Pipeline)
			}
		})
	}
}

func TestWaitEmptyTagPipelineAfterContinuation(t *testing.T) {
	fx := emptyPipelineFixture(t, "pending", true)
	route := "GET /api/v2/project/gh/giantswarm/kserve/pipeline"
	fx.circleci[route] = append(fx.circleci[route], sequence.Response{Body: map[string]any{
		"items": []map[string]any{{"id": "p1", "number": 12, "state": "created", "vcs": map[string]any{"tag": testTag, "revision": testSHA}}},
	}})
	clock := agentcli.NewVirtualClock(time.Now())
	start := clock.Now()
	fx.clock = &clock
	var counter *emptyPipelinePolls
	fx.wrapCircleCI = func(c CircleCI) CircleCI {
		counter = &emptyPipelinePolls{CircleCI: c}
		return counter
	}
	_, err := run(t, fx)
	assertExit(t, err, agentcli.ExitTagPipelineEmpty, "created with no workflows")
	if counter.polls != 2 || clock.Now().Sub(start) != IntervalFloor {
		t.Fatalf("want one pending poll before the empty verdict: %d polls, %s", counter.polls, clock.Now().Sub(start))
	}
}

func TestWaitEmptyTagPipelineNeedsAdmittedJobs(t *testing.T) {
	cases := []struct {
		name   string
		config string
	}{
		{name: "case 0: branch jobs alone", config: "workflows:\n  build:\n    jobs: [build]\n"},
		{name: "case 1: another tag", config: strings.ReplaceAll(emptyPipelineConfig, "only: /^v[0-9]+\\.[0-9]+\\.[0-9]+(-rc\\.[0-9]+)?$/", "only: v9.9.9")},
		{name: "case 2: ignored tag", config: strings.ReplaceAll(emptyPipelineConfig, "only: /^v[0-9]+\\.[0-9]+\\.[0-9]+(-rc\\.[0-9]+)?$/", "ignore: /.*/")},
		{name: "case 3: disabled workflow", config: strings.ReplaceAll(emptyPipelineConfig, "  publish:\n", "  publish:\n    when: false\n")},
		{name: "case 4: unresolved condition", config: strings.ReplaceAll(emptyPipelineConfig, "  publish:\n", "  publish:\n    when: << pipeline.parameters.publish >>\n")},
		{name: "case 5: no configuration"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := emptyPipelineFixture(t, "created", true)
			fx.github[repoRoute("/contents/.circleci/config.yml?ref="+testSHA)] = []sequence.Response{{Body: fileContent(".circleci/config.yml", tc.config)}}
			if tc.config == "" {
				delete(fx.github, repoRoute("/contents/.circleci/config.yml?ref="+testSHA))
			}
			clock := agentcli.NewVirtualClock(time.Now())
			start := clock.Now()
			fx.clock, fx.timeout = &clock, 30*time.Second
			_, err := run(t, fx)
			assertExit(t, err, agentcli.ExitTimeout, "")
			if clock.Now().Sub(start) < fx.timeout {
				t.Fatalf("wait ended early: %s", clock.Now().Sub(start))
			}
		})
	}
}

func TestWaitEmptyTagPipelineConfigReadFailure(t *testing.T) {
	fx := emptyPipelineFixture(t, "created", false)
	fx.github[repoRoute("/contents/.circleci/config.yml?ref="+testSHA)] = []sequence.Response{{Status: 403, Body: map[string]any{"message": "configuration read refused"}}}
	result, err := run(t, fx)
	assertExit(t, err, agentcli.ExitUsage, "configuration read refused")
	if result.Pipeline == nil || result.Pipeline.Number != 12 {
		t.Fatalf("read failure lost pipeline evidence: %+v", result.Pipeline)
	}
}
