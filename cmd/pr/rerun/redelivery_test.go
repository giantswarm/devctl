package rerun

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/rerun"
)

func headPipeline(id string, number int, revision string) map[string]any {
	return map[string]any{
		"id": id, "number": number, "state": "pending", "created_at": "2026-10-09T17:00:00Z",
		"vcs": map[string]any{"revision": revision},
	}
}

type observationTransport func(*http.Request) (*http.Response, error)

func (f observationTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRedeliveryObservationInterrupted(t *testing.T) {
	testCases := []struct {
		name   string
		cancel bool
	}{
		{name: "case 0: parent cancellation interrupts observation", cancel: true},
		{name: "case 1: the observation budget interrupts a stalled read"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			clock := agentcli.NewVirtualClock(time.Now())
			calls := 0
			client, err := circleciclient.New(circleciclient.Config{
				Token: "cci_test",
				Transport: observationTransport(func(req *http.Request) (*http.Response, error) {
					calls++
					if tc.cancel {
						cancel()
					} else if err := clock.Sleep(req.Context(), time.Minute); err == nil {
						t.Error("observation request had no bounded context")
					}
					return nil, req.Context().Err()
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			doc := document{Document: rerun.NewDocument(command, "o/r")}
			doc.HeadSHA = "abc"
			doc.Pipeline = &rerun.Pipeline{ID: "p1", Number: 12}
			doc.Redelivery = &rerun.RedeliveryResult{Outcome: rerun.OutcomeRedelivered}
			err = awaitRedelivery(ctx, client, "o", "r", "feature", &doc, clock)
			if err == nil || agentcli.Exit(err) != agentcli.ExitUsage || !strings.Contains(err.Error(), "recovery is unconfirmed") || calls != 1 {
				t.Fatalf("err %v, calls %d; want one interrupted read with an unconfirmed recovery", err, calls)
			}
			if doc.Redelivery.Outcome != rerun.OutcomeRedelivered || doc.Redelivery.Pipeline != nil {
				t.Errorf("unconfirmed read claimed an outcome: %+v", doc.Redelivery)
			}
		})
	}
}

func pipelineListing(items ...any) sequence.Response {
	return sequence.Response{Body: map[string]any{"items": items}}
}

func TestRedeliveryMustCreatePipeline(t *testing.T) {
	testCases := []struct {
		name         string
		listings     []sequence.Response
		wantCode     int
		wantReason   string
		wantPipeline bool
		wantElapsed  time.Duration
	}{
		{
			name:     "case 0: accepted push is deduplicated",
			listings: []sequence.Response{pipelineListing(headPipeline("p1", 12, "abc"))},
			wantCode: agentcli.ExitRefused, wantReason: "redelivery deduplicated; push a new head", wantElapsed: time.Minute,
		},
		{
			name:     "case 1: listing loses the head pipeline",
			listings: []sequence.Response{pipelineListing(headPipeline("p1", 12, "abc")), pipelineListing()},
			wantCode: agentcli.ExitRefused, wantReason: "redelivery deduplicated; push a new head", wantElapsed: time.Minute,
		},
		{
			name:     "case 2: a newer pipeline built another head",
			listings: []sequence.Response{pipelineListing(headPipeline("p1", 12, "abc")), pipelineListing(headPipeline("p2", 13, "other"))},
			wantCode: agentcli.ExitRefused, wantReason: "redelivery deduplicated; push a new head", wantElapsed: time.Minute,
		},
		{
			name:     "case 3: a stale listing returns an older pipeline",
			listings: []sequence.Response{pipelineListing(headPipeline("p1", 12, "abc")), pipelineListing(headPipeline("p0", 11, "abc"))},
			wantCode: agentcli.ExitRefused, wantReason: "redelivery deduplicated; push a new head", wantElapsed: time.Minute,
		},
		{
			name: "case 4: a new pipeline becomes visible after a delay",
			listings: []sequence.Response{
				pipelineListing(headPipeline("p1", 12, "abc")),
				pipelineListing(headPipeline("p1", 12, "abc")),
				pipelineListing(headPipeline("p2", 13, "other")),
				pipelineListing(headPipeline("p2", 13, "abc")),
			},
			wantCode: agentcli.ExitOK, wantPipeline: true, wantElapsed: 10 * time.Second,
		},
		{
			name: "case 5: a refused observation is not deduplication",
			listings: []sequence.Response{
				pipelineListing(headPipeline("p1", 12, "abc")),
				{Status: http.StatusForbidden, Body: map[string]any{"message": "observation refused"}},
			},
			wantCode: agentcli.ExitUsage, wantReason: "observation refused",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			r, gh, cc := start(t,
				sequence.Routes{
					"GET /repos/o/r/pulls/7": pull("o/r"),
					"GET /repos/o/r/hooks": {{Body: []any{map[string]any{
						"id": 1, "active": true, "config": map[string]any{"url": "https://circleci.com/hooks/github"},
					}}}},
					"GET /repos/o/r/hooks/1/deliveries": {{Body: []any{map[string]any{
						"id": 42, "guid": "g-push", "delivered_at": "2026-10-09T17:59:00Z", "event": "push",
					}}}},
					"GET /repos/o/r/hooks/1/deliveries/42": {{Body: map[string]any{
						"id": 42, "guid": "g-push", "delivered_at": "2026-10-09T17:59:00Z", "event": "push",
						"request": map[string]any{"payload": map[string]any{"ref": "refs/heads/feature", "after": "abc"}},
					}}},
					"POST /repos/o/r/hooks/1/deliveries/42/attempts": {{Status: http.StatusAccepted}},
				},
				sequence.Routes{
					"GET /api/v2/project/gh/o/r/pipeline?branch=feature": tc.listings,
					"GET /api/v2/pipeline/p1/workflow":                   {{Body: map[string]any{"items": []any{}}}},
				})
			start := time.Date(2026, 10, 9, 18, 30, 0, 0, time.UTC)
			clock := agentcli.NewVirtualClock(start)
			r.clock = func() (agentcli.Clock, error) { return clock, nil }
			doc, code := execute(t, r, "o/r", "7")
			if code != tc.wantCode || !strings.Contains(doc["reason"].(string), tc.wantReason) {
				t.Errorf("exit %d, reason %q; want %d containing %q", code, doc["reason"], tc.wantCode, tc.wantReason)
			}
			if elapsed := clock.Now().Sub(start); elapsed != tc.wantElapsed {
				t.Errorf("elapsed %s, want %s", elapsed, tc.wantElapsed)
			}
			redelivery := doc["redelivery"].(map[string]any)
			wantOutcome := "redelivered"
			if tc.wantCode == agentcli.ExitRefused {
				wantOutcome = "deduplicated"
			}
			if redelivery["outcome"] != wantOutcome || redelivery["deliveryId"] != float64(42) || doc["headSha"] != "abc" || doc["pipeline"].(map[string]any)["id"] != "p1" {
				t.Errorf("lost outcome or original evidence: %v", doc)
			}
			p, hasPipeline := redelivery["pipeline"].(map[string]any)
			if hasPipeline != tc.wantPipeline || hasPipeline && (p["id"] != "p2" || p["number"] != float64(13) || p["url"] != "https://app.circleci.com/pipelines/github/o/r/13") {
				t.Errorf("new pipeline: %v, want present=%v", p, tc.wantPipeline)
			}
			if tc.wantCode == agentcli.ExitRefused && (!strings.Contains(doc["reason"].(string), "empty commit") || !strings.Contains(doc["reason"].(string), "merge")) {
				t.Errorf("missing recovery: %v", doc["reason"])
			}
			if tc.wantPipeline && !strings.Contains(fmt.Sprint(doc["warnings"]), "pipeline 13") {
				t.Errorf("missing confirmed pipeline warning: %v", doc["warnings"])
			}
			posts := 0
			for _, req := range gh.Requests() {
				if req.Method == http.MethodPost {
					posts++
				}
			}
			if posts != 1 {
				t.Errorf("redelivery writes: %d, want one", posts)
			}
			for _, req := range cc.Requests() {
				if req.Method != http.MethodGet {
					t.Errorf("unexpected CircleCI write: %v", req)
				}
			}
		})
	}
}
