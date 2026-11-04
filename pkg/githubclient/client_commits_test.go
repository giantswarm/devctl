package githubclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sirupsen/logrus"
)

func Test_CommitsBetween_stopsWithoutPartialHistory(t *testing.T) {
	for _, tc := range []struct {
		name       string
		failPage   int
		wantReads  int
		wantReason string
	}{
		{name: "case 0: a later page fails", failPage: 2, wantReads: 2, wantReason: "cannot read page"},
		{name: "case 1: pagination exceeds the bound", wantReads: maxPages, wantReason: "exceeds 10 pages"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var reads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				page := int(reads.Add(1))
				if r.Method != http.MethodGet || r.URL.Path != "/repos/o/r/compare/base...head" || r.URL.Query().Get("per_page") != "100" || r.URL.Query().Get("page") != fmt.Sprint(page) {
					t.Errorf("want immutable comparison with the next page, got %s %s", r.Method, r.URL)
				}
				w.Header().Set("Content-Type", "application/json")
				if page == tc.failPage {
					w.WriteHeader(http.StatusForbidden)
					_, _ = io.WriteString(w, `{"message":"cannot read page"}`)
					return
				}
				w.Header().Set("Link", fmt.Sprintf("</repos/o/r/compare/base...head?page=%d&per_page=100>; rel=\"next\"", page+1))
				_, _ = io.WriteString(w, `{"total_commits":1001,"commits":[{"sha":"one","commit":{"message":"fix: clean"}}]}`)
			}))
			t.Cleanup(server.Close)
			logger := logrus.New()
			logger.SetOutput(io.Discard)
			client, _, err := NewConditional(Config{Logger: logger, AccessToken: "test", BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			commits, err := client.CommitsBetween(context.Background(), "o", "r", "base", "head")
			if err == nil || !strings.Contains(err.Error(), tc.wantReason) || commits != nil || int(reads.Load()) != tc.wantReads {
				t.Fatalf("want no partial history, %d reads and error %q; got %v, %d, %v", tc.wantReads, tc.wantReason, commits, reads.Load(), err)
			}
		})
	}
}
