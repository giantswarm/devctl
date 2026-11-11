package rerun

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

type wedgedCancelTransport func(*http.Request) (*http.Response, error)

func (f wedgedCancelTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// Stopping the caller during the cancel wait must not enter the recovery
// reserved for a cancel that CircleCI has ignored for two minutes.
func TestWedgedCancellationDoesNotTriggerFreshPipeline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var writes []string
	client, err := circleciclient.New(circleciclient.Config{
		Token: "token_test",
		Transport: wedgedCancelTransport(func(req *http.Request) (*http.Response, error) {
			var body string
			switch req.Method + " " + req.URL.Path {
			case "GET /api/v2/pipeline/p1/workflow":
				body = `{"items":[{"id":"w1","name":"build","status":"running","created_at":"2026-10-09T10:00:00Z"}]}`
			case "POST /api/v2/workflow/w1/cancel":
				writes = append(writes, req.URL.Path)
				body = `{"message":"Accepted."}`
			case "GET /api/v2/workflow/w1":
				cancel()
				return nil, req.Context().Err()
			default:
				t.Fatalf("unexpected request: %s %s", req.Method, req.URL.Path)
			}
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	triggered := false
	redelivery := Redelivery{Trigger: func(ctx context.Context) (*circleciclient.Pipeline, error) {
		triggered = true
		return nil, ctx.Err()
	}}
	result := NewResult("o/r")
	err = FromFailed(ctx, client, "o", "r", &circleciclient.Pipeline{ID: "p1", Number: 12}, result, func(string) {}, redelivery,
		Wedged{After: time.Hour, Clock: agentcli.NewVirtualClock(time.Date(2026, 10, 9, 18, 0, 0, 0, time.UTC))})
	if !errors.Is(err, context.Canceled) || triggered || result.Trigger != nil {
		t.Errorf("err=%v triggered=%v trigger=%+v; want caller cancellation without fresh-pipeline recovery", err, triggered, result.Trigger)
	}
	if len(writes) != 1 || writes[0] != "/api/v2/workflow/w1/cancel" || len(result.Workflows) != 1 || !result.Workflows[0].Canceled || result.Workflows[0].Outcome == OutcomeRunning {
		t.Errorf("writes=%v workflows=%+v; want the acknowledged cancel retained without a timeout outcome", writes, result.Workflows)
	}
}
