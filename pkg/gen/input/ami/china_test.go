package ami

import (
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// Serves the S3 GetObject API on a local listener; the bucket is addressed
// virtual-host style as bucket.localhost.
func fakeS3(t *testing.T, handler http.HandlerFunc) {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(handler)
	srv.Listener = l
	srv.Start()
	t.Cleanup(srv.Close)

	_, port, _ := net.SplitHostPort(l.Addr().String())
	t.Setenv("AWS_ENDPOINT_URL_S3", "http://localhost:"+port)
}

func Test_getChinaFlatcarRelease(t *testing.T) {
	config := Config{
		Arch:                    "amd64-usr",
		Channel:                 "stable",
		ChinaBucketName:         "bucket",
		ChinaBucketRegion:       "cn-north-1",
		ChinaAWSAccessKeyID:     "id",
		ChinaAWSSecretAccessKey: "secret",
	}

	t.Run("found", func(t *testing.T) {
		fakeS3(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/stable/amd64-usr/1.2.3.json" {
				t.Errorf("unexpected path %q", r.URL.Path)
			}
			if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=id/") {
				t.Errorf("request not signed with the static credentials")
			}
			_, _ = w.Write([]byte(`{"amis":[{"name":"cn-north-1","hvm":"ami-123"}]}`))
		})

		got, err := getChinaFlatcarRelease(config, "1.2.3")
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"cn-north-1": "ami-123"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("not found", func(t *testing.T) {
		fakeS3(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>NoSuchKey</Code><Message>missing</Message></Error>`))
		})

		got, err := getChinaFlatcarRelease(config, "1.2.3")
		if err != nil || got != nil {
			t.Fatalf("got %v, %v; want nil, nil", got, err)
		}
	})

	t.Run("other error", func(t *testing.T) {
		fakeS3(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>AccessDenied</Code><Message>no</Message></Error>`))
		})

		if _, err := getChinaFlatcarRelease(config, "1.2.3"); err == nil {
			t.Fatal("expected an error")
		}
	})
}
