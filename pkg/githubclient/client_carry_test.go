package githubclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

func Test_CommitsBetween(t *testing.T) {
	tests := []struct {
		name       string
		lastStatus int
		total      int
		endless    bool
		wantReason string
	}{
		{name: "case 0: every page of an immutable comparison", total: 2},
		{name: "case 1: a truncated comparison returns no commits", total: 3, wantReason: "incomplete commit comparison"},
		{name: "case 2: a later page fails", total: 2, lastStatus: http.StatusForbidden, wantReason: "403"},
		{name: "case 3: a page limit never returns partial evidence", total: 1001, endless: true, wantReason: "exceeds 10 pages"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != http.MethodGet || r.URL.Path != "/repos/o/r/compare/base123...head123" || r.URL.Query().Get("per_page") != "100" {
					t.Errorf("comparison moved or was not paginated: %s", r.URL)
				}
				w.Header().Set("Content-Type", "application/json")
				if requests == 1 || tc.endless {
					w.Header().Set("Link", fmt.Sprintf(`<http://%s/repos/o/r/compare/base123...head123?page=%d>; rel="next"`, r.Host, requests+1))
				} else if tc.lastStatus != 0 {
					w.WriteHeader(tc.lastStatus)
					_, _ = io.WriteString(w, `{"message":"unreadable"}`)
					return
				}
				_, _ = fmt.Fprintf(w, `{"total_commits":%d,"commits":[{"sha":"c%d","author":{"id":%d}}]}`, tc.total, requests, requests)
			}))
			t.Cleanup(server.Close)
			logger := logrus.New()
			logger.SetOutput(io.Discard)
			c, _, err := NewConditional(Config{Logger: logger, AccessToken: "ghu_test", BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			commits, err := c.CommitsBetween(context.Background(), "o", "r", "base123", "head123")
			if tc.wantReason != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantReason) || len(commits) != 0 {
					t.Fatalf("want no partial commits and error %q, got %v, %v", tc.wantReason, commits, err)
				}
				return
			}
			if err != nil || len(commits) != 2 || commits[0].GetSHA() != "c1" || commits[1].GetAuthor().GetID() != 2 || requests != 2 {
				t.Fatalf("want both pages and their authors, got %v, %v (%d requests)", commits, err, requests)
			}
		})
	}
}
