package ami

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type cancellationTransport func(*http.Request) (*http.Response, error)

func (f cancellationTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type cancelListingReader struct {
	cancel context.CancelFunc
}

func (r cancelListingReader) Read([]byte) (int, error) {
	r.cancel()
	return 0, context.Canceled
}

func cancellationConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		Arch: "amd64-usr", Channel: "stable", PrimaryDomain: "fixture.invalid",
		ChinaBucketName: "fixture", ChinaBucketRegion: "cn-north-1",
		ChinaAWSAccessKeyID: "fixture", ChinaAWSSecretAccessKey: "fixture", // #nosec G101 -- Synthetic credentials for loopback tests only.
		Dir: t.TempDir(), MinimumVersion: "1.0.0",
	}
}

func TestBootHonorsHTTPCancellation(t *testing.T) {
	for _, stage := range []string{"before boot", "listing request", "listing body", "release request"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if stage == "before boot" {
				cancel()
			}
			original := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = original })
			http.DefaultTransport = cancellationTransport(func(r *http.Request) (*http.Response, error) {
				isRelease := strings.HasSuffix(r.URL.Path, ".json")
				if stage == "before boot" {
					t.Error("canceled Boot started an HTTP request")
				}
				if stage == "listing request" || stage == "release request" && isRelease {
					cancel()
					if err := r.Context().Err(); err != nil {
						return nil, err
					}
					return nil, errors.New("HTTP request detached from Boot context")
				}
				var body io.Reader = strings.NewReader("")
				if stage == "listing body" {
					body = cancelListingReader{cancel: cancel}
				} else if stage == "release request" {
					body = strings.NewReader(`<a href="1.2.3/">release</a>`)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(body), Header: make(http.Header)}, nil
			})

			a, err := New(cancellationConfig(t))
			if err != nil {
				t.Fatal(err)
			}
			if err := a.Boot(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("Boot returned %v; want context.Canceled", err)
			}
			if a.booted {
				t.Fatal("canceled Boot marked input ready to generate")
			}
		})
	}
}

func TestBootCancelsChinaRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseServer := func() { releaseOnce.Do(func() { close(release) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done():
			return
		case <-release:
			_, _ = io.WriteString(w, `{"amis":[{"name":"cn-north-1","hvm":"ami-china-fixture"}]}`)
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(releaseServer)

	// Isolate SDK configuration and credentials; every S3 request goes to loopback.
	awsDir := t.TempDir()
	configPath := filepath.Join(awsDir, "config")
	credentialsPath := filepath.Join(awsDir, "credentials")
	for _, name := range []string{configPath, credentialsPath} {
		if err := os.WriteFile(name, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("AWS_CONFIG_FILE", configPath)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credentialsPath)
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_CA_BUNDLE", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_ENDPOINT_URL_S3", server.URL)
	t.Setenv("AWS_IGNORE_CONFIGURED_ENDPOINT_URLS", "false")
	t.Setenv("AWS_USE_FIPS_ENDPOINT", "false")
	t.Setenv("AWS_USE_DUALSTACK_ENDPOINT", "false")
	t.Setenv("AWS_DEFAULTS_MODE", "standard")

	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = cancellationTransport(func(r *http.Request) (*http.Response, error) {
		body := `<a href="1.2.3/">release</a>`
		if strings.HasSuffix(r.URL.Path, ".json") {
			body = `{"amis":[{"name":"eu-west-1","hvm":"ami-global-fixture"}]}`
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	a, err := New(cancellationConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- a.Boot(ctx) }()
	select {
	case <-started:
		cancel()
	case err := <-done:
		t.Fatalf("Boot returned before China request started: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("China request did not start")
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Boot returned %v; want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		releaseServer()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("Boot did not exit after fixture was released")
		}
		t.Fatal("Boot ignored cancellation during China request")
	}
	if a.booted {
		t.Fatal("canceled Boot marked input ready to generate")
	}
}
