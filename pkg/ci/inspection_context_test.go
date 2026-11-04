package ci

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

type inspectionTransport func(*http.Request) (*http.Response, error)

func (f inspectionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// Unavailable optional data may degrade to warnings or logError. A caller
// canceling its inspection must instead get the cancellation error and keep
// the metadata already read, without starting another request.
func TestInspectionPreservesCallerContext(t *testing.T) {
	for _, tc := range []struct {
		name       string
		jobs       bool
		path       string
		occurrence int
	}{
		{name: "case 0: pipeline job steps", jobs: true, path: "/api/v1.1/project/github/o/r/4", occurrence: 1},
		{name: "case 1: pipeline step output", jobs: true, path: "/push", occurrence: 1},
		{name: "case 2: single job steps", path: "/api/v1.1/project/github/o/r/7", occurrence: 1},
		{name: "case 3: failed log metadata", path: "/api/v1.1/project/github/o/r/7", occurrence: 2},
		{name: "case 4: failed step output", path: "/build", occurrence: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			routes := routes7("https://ci-output.invalid")
			routes["GET /build"] = []sequence.Response{{Body: buildOutput}}
			if tc.jobs {
				routes = routes3885("https://ci-output.invalid")
				routes["GET /api/v2/pipeline/p-3885/workflow"] = []sequence.Response{{Body: map[string]any{"items": []any{
					run("w3", "build", "running", "2026-10-09T17:20:00Z"),
				}}}}
				routes["GET /push"] = []sequence.Response{{Body: pushOutput}}
			}
			script, err := sequence.New(routes)
			if err != nil {
				t.Fatal(err)
			}
			matched, afterCancel := 0, 0
			client, err := circleciclient.New(circleciclient.Config{
				Token: "token_test",
				Transport: inspectionTransport(func(req *http.Request) (*http.Response, error) {
					if ctx.Err() != nil {
						afterCancel++
						return nil, ctx.Err()
					}
					if req.URL.Path == tc.path {
						matched++
						if matched == tc.occurrence {
							cancel()
							return nil, req.Context().Err()
						}
					}
					resp, ok := script.Next(req)
					if !ok {
						t.Fatalf("unexpected request: %s %s", req.Method, req.URL.Path)
					}
					return inspectionResponse(t, req, resp), nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			if tc.jobs {
				result := NewJobsResult("o/r")
				err = Jobs(ctx, client, "o", "r", "3885", now, result, func(string) {})
				if result.Pipeline == nil || len(result.Workflows) != 1 || len(result.Workflows[0].Jobs) < 2 || result.Workflows[0].Jobs[1].Name != "push" {
					t.Errorf("partial result: %+v; want pipeline, workflow, lint and push metadata", result)
				}
			} else {
				result := NewJobResult("o/r")
				err = ReadJob(ctx, client, "o", "r", "7", DefaultJobLogLines, result, func(string) {})
				if result.Job == nil || result.Job.Number != 7 {
					t.Errorf("partial result: %+v; want job 7 metadata", result)
				}
			}
			if !errors.Is(err, context.Canceled) || afterCancel != 0 || matched != tc.occurrence {
				t.Fatalf("err=%v afterCancel=%d matched=%d; want caller cancellation at request %d and no subsequent requests", err, afterCancel, matched, tc.occurrence)
			}
		})
	}
}

func inspectionResponse(t *testing.T, req *http.Request, response sequence.Response) *http.Response {
	t.Helper()
	body, err := response.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return &http.Response{StatusCode: response.StatusCode(), Header: response.Header(), Body: io.NopCloser(strings.NewReader(string(body))), Request: req}
}
