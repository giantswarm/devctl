package githubclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func Test_Conditional(t *testing.T) {
	// The server answers its body with an ETag and 304 to a matching
	// If-None-Match, the way api.github.com does; the budget shrinks by one
	// per full answer.
	remaining := 100
	reset := time.Now().Add(time.Hour).Unix()
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Header.Get("If-None-Match"))
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset, 10))
		w.Header().Set("ETag", `"v1"`)
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		remaining--
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"n":1}`))
	}))
	defer server.Close()

	c := &Conditional{}
	client := &http.Client{Transport: c}
	get := func() (int, string) {
		resp, err := client.Get(server.URL + "/repos/o/r/pulls/1")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}

	if code, body := get(); code != 200 || body != `{"n":1}` {
		t.Fatalf("first: want 200 {\"n\":1}, got %d %s", code, body)
	}
	if code, body := get(); code != 200 || body != `{"n":1}` {
		t.Fatalf("replay: want 200 with the cached body, got %d %s", code, body)
	}
	if want := []string{"", `"v1"`}; len(requests) != 2 || requests[1] != want[1] {
		t.Errorf("If-None-Match per request: want %q, got %q", want, requests)
	}
	if c.Replayed() != 1 {
		t.Errorf("replayed: want 1, got %d", c.Replayed())
	}
	rate := c.Rate()
	if !rate.Known || rate.Remaining != 99 || rate.Reset.Unix() != reset {
		t.Errorf("rate: want known, 99 remaining, reset %d; got %+v", reset, rate)
	}
}

func Test_NewConditional_SendsPastASpentBudget(t *testing.T) {
	// The first answer spends the budget. go-github would refuse the second
	// request itself until the reset; the poll client sends it, so the
	// transport under it decides.
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		remaining := "0"
		if requests > 1 {
			remaining = "4999"
		}
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", remaining)
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"number":1,"state":"open"}`))
	}))
	defer server.Close()

	client, conditional, err := NewConditional(Config{Logger: logrus.New(), AccessToken: "ghu_test", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		if _, err := client.PullRequest(context.Background(), "o", "r", 1); err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
	}
	if requests != 2 {
		t.Errorf("requests: want 2, got %d", requests)
	}
	if rate := conditional.Rate(); rate.Remaining != 4999 {
		t.Errorf("rate: want the second answer's 4999 remaining, got %+v", rate)
	}
}
