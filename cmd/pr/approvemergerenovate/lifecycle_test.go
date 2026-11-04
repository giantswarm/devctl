package approvemergerenovate

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/synctest"

	"github.com/sirupsen/logrus"
)

type discoveryTransport func(*http.Request) (*http.Response, error)

func (f discoveryTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// The last initial worker finishes while discovery is still reading. The
// command must own that read and any workers it adds until they finish.
func TestCommandWaitsForInFlightDiscovery(t *testing.T) {
	t.Setenv("DEVCTL_GITHUB_TOKEN", "synthetic-offline-token")
	_, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	for _, mode := range []string{"new PR", "empty search", "failed search"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				pollStarted, releaseSearch := make(chan struct{}), make(chan struct{})
				secondStarted, releaseSecond := make(chan struct{}), make(chan struct{})
				searches := 0
				var returned atomic.Bool
				var second atomic.Int32
				previous := http.DefaultTransport
				http.DefaultTransport = discoveryTransport(func(req *http.Request) (*http.Response, error) {
					var body any
					code := http.StatusOK
					switch req.URL.Path {
					case "/search/issues":
						searches++
						n := "1"
						if searches == 2 {
							close(pollStarted)
							<-releaseSearch
							n = "2"
						}
						body = map[string]any{"total_count": 1, "items": []any{map[string]any{"number": searches, "title": "Update fixture", "html_url": "https://github.com/o/r/pull/" + n}}}
						if searches == 2 && mode != "new PR" {
							body = map[string]any{"total_count": 0, "items": []any{}}
							if mode == "failed search" {
								code = http.StatusServiceUnavailable
							}
						}
					case "/repos/o/r/pulls/1":
						<-pollStarted
						body = map[string]any{"merged": true}
					case "/repos/o/r/pulls/2":
						second.Add(1)
						close(secondStarted)
						<-releaseSecond
						body = map[string]any{"merged": true}
					default:
						t.Errorf("unexpected offline request: %s %s", req.Method, req.URL)
						body = map[string]any{}
					}
					raw, err := json.Marshal(body)
					if err != nil {
						return nil, err
					}
					return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: req}, nil
				})
				defer func() { http.DefaultTransport = previous }()
				logger := logrus.New()
				logger.SetOutput(io.Discard)
				cmd, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
				if err != nil {
					t.Fatal(err)
				}
				cmd.SetArgs([]string{"fixture", "--dry-run"})
				done := make(chan error, 1)
				go func() { err := cmd.Execute(); returned.Store(true); done <- err }()
				<-pollStarted
				synctest.Wait()
				if returned.Load() {
					t.Error("command returned while discovery could still start new PR workers")
				}
				close(releaseSearch)
				if mode == "new PR" {
					<-secondStarted
					synctest.Wait()
					if returned.Load() {
						t.Error("command returned while a discovered PR worker was still active")
					}
					close(releaseSecond)
				}
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				synctest.Wait()
				want := int32(0)
				if mode == "new PR" {
					want = 1
				}
				if second.Load() != want {
					t.Errorf("discovered PR processed %d times, want %d", second.Load(), want)
				}
			})
		})
	}
}
