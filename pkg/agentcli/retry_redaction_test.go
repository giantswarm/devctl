package agentcli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type redactionTransport func(*http.Request) (*http.Response, error)

func (f redactionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// A failed log download can recover, but its signed URL must not be copied
// into the command's warnings or progress. The actual requests still need it.
func TestRetryingWarningOmitsURLCredentials(t *testing.T) {
	const target = "https://synthetic-user:synthetic-password@logs.example.invalid/job.txt?sig=synthetic-signature&token=synthetic-token#synthetic-fragment"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	var progress bytes.Buffer
	doc := NewEnvelope("pr wait", time.Now())
	calls := 0
	retrying := &Retrying{
		Clock:    NewVirtualClock(time.Now()),
		Progress: NewProgress(&progress, true),
		Warn:     doc.Warn,
		Base: redactionTransport(func(got *http.Request) (*http.Response, error) {
			calls++
			if got.URL.String() != target {
				t.Errorf("request %d lost its URL credentials", calls)
			}
			code := http.StatusServiceUnavailable
			if calls > 1 {
				code = http.StatusOK
			}
			return &http.Response{
				StatusCode: code,
				Status:     http.StatusText(code),
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("answer")),
			}, nil
		}),
	}
	resp, err := retrying.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if calls != 2 || resp.StatusCode != http.StatusOK || len(doc.Warnings) != 1 {
		t.Fatalf("want recovery after one retry, got %d calls, status %d, %d warnings", calls, resp.StatusCode, len(doc.Warnings))
	}
	var output bytes.Buffer
	if err := Report(&output, &doc, VerdictRed, nil); err != nil {
		t.Fatal(err)
	}
	for name, message := range map[string]string{"document": output.String(), "progress": progress.String()} {
		if !strings.Contains(message, "GET https://logs.example.invalid/job.txt: ") || !strings.Contains(message, "Service Unavailable") {
			t.Errorf("%s lost the request path or failure", name)
		}
		for _, sensitive := range []string{"synthetic-user", "synthetic-password", "synthetic-signature", "synthetic-token", "synthetic-fragment"} {
			if strings.Contains(message, sensitive) {
				t.Errorf("%s contains URL credentials or fragment: %s", name, sensitive)
			}
		}
	}
}
