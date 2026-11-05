package agentcli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRetryingHEADPreservesContentLength(t *testing.T) {
	testCases := []struct {
		name    string
		answers []answer
	}{
		{name: "case 0: immediate answer"},
		{name: "case 1: answer after a temporary failure", answers: []answer{status(http.StatusServiceUnavailable)}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// A HEAD has no body, but its Content-Length describes the GET's
			// representation, which a metadata probe needs to keep.
			answers := append(tc.answers, func(w http.ResponseWriter) {
				w.Header().Set("Content-Length", "4096")
				w.WriteHeader(http.StatusOK)
			})
			s := &script{answers: answers}
			server := httptest.NewServer(s)
			defer server.Close()
			client := &http.Client{Transport: &Retrying{Clock: NewVirtualClock(time.Now()), Attempts: 2}}
			resp, err := client.Head(server.URL + "/artifact")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK || s.count() != len(answers) {
				t.Fatalf("want 200 after %d requests, got %d after %d", len(answers), resp.StatusCode, s.count())
			}
			if resp.ContentLength != 4096 || resp.Header.Get("Content-Length") != "4096" {
				t.Errorf("want HEAD representation length 4096, got ContentLength=%d, header=%q", resp.ContentLength, resp.Header.Get("Content-Length"))
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil || len(body) != 0 {
				t.Fatalf("want an empty HEAD body, got %q, %v", body, err)
			}
		})
	}
}
