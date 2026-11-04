package ami_test

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/giantswarm/micrologger"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/gen/ami"
	"github.com/giantswarm/devctl/v8/pkg/gen"
)

// Exercise the public command, including the real input and generator layers.
// Do not parallelize: http.DefaultClient and the SDK environment are isolated
// per case. Neither transport can reach a production release source or AWS.
func TestCommand_Generation(t *testing.T) {
	const original = `{"3000.0.0":{"eu-west-1":"ami-unlisted"},"5000.0.0":{"eu-west-1":"ami-kept"}}`
	fetchErr := errors.New("synthetic release fetch failure")
	testCases := []struct {
		name           string
		keepExisting   bool
		globalFailure  bool
		chinaFailure   bool
		destinationDir bool
		wantErr        string
	}{
		{name: "successful update replaces the old destination"},
		{name: "keep existing can read the destination before replacing it", keepExisting: true},
		{name: "later global fetch failure preserves the destination", globalFailure: true, wantErr: fetchErr.Error()},
		{name: "later China failure preserves the destination", chinaFailure: true, wantErr: "InvalidAccessKeyId"},
		{name: "destination error reaches the caller", destinationDir: true, wantErr: "is a directory"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var globalRequests, chinaRequests []string
			china := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				chinaRequests = append(chinaRequests, r.URL.Path)
				mu.Unlock()
				// Check the nondefault region without printing the signed header.
				if r.Method != http.MethodGet || !strings.Contains(r.Header.Get("Authorization"), "/cn-northwest-1/s3/aws4_request") {
					t.Error("expected a GetObject signed for the configured China region")
				}
				if tc.chinaFailure && strings.HasSuffix(r.URL.Path, "/5001.0.0.json") {
					w.Header().Set("Content-Type", "application/xml")
					w.WriteHeader(http.StatusForbidden)
					_, _ = io.WriteString(w, `<Error><Code>InvalidAccessKeyId</Code><Message>synthetic lookup failure</Message></Error>`)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"amis":[{"name":"cn-northwest-1","hvm":"ami-china"}]}`)
			}))
			t.Cleanup(china.Close)
			isolateAWS(t, china.URL)

			previousClient := http.DefaultClient
			http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				globalRequests = append(globalRequests, r.URL.String())
				const base = "https://beta.release.ami-fixture.invalid/arm64-usr/"
				var body string
				switch r.URL.String() {
				case base:
					body = `<a href="./4999.0.0/">below minimum</a><a href="./5000.0.0/">first</a><a href="./5001.0.0/">second</a>`
				case base + "5000.0.0/flatcar_production_ami_all.json":
					body = `{"amis":[{"name":"eu-west-1","hvm":"ami-global-5000"}]}`
				case base + "5001.0.0/flatcar_production_ami_all.json":
					if tc.globalFailure {
						return nil, fetchErr
					}
					body = `{"amis":[{"name":"eu-west-1","hvm":"ami-global-5001"}]}`
				default:
					return nil, fmt.Errorf("unexpected fixture request: %s", r.URL)
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			t.Cleanup(func() { http.DefaultClient = previousClient })

			dir := t.TempDir()
			destination := filepath.Join(dir, "aws-ami.yaml.template")
			if tc.destinationDir {
				require.NoError(t, os.Mkdir(destination, 0o750))
			} else {
				require.NoError(t, os.WriteFile(destination, []byte(original), 0o600))
			}
			logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
			require.NoError(t, err)
			command, err := ami.New(ami.Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
			require.NoError(t, err)
			command.SilenceErrors = true
			command.SilenceUsage = true
			args := []string{
				"--dir=" + dir,
				"--arch=arm64-usr", "--stable=beta", "--primary.domain=ami-fixture.invalid",
				"--minimum.version=5000.0.0", "--china.bucket=ami-fixture-bucket", "--china.region=cn-northwest-1",
				"--aws.accesskeyid=synthetic-access-key", "--aws.secretaccesskey=synthetic-secret-key",
			}
			if tc.keepExisting {
				args = append(args, "--keep.existing="+destination)
			}
			command.SetArgs(args)
			err = command.ExecuteContext(t.Context())

			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				if tc.globalFailure {
					require.ErrorIs(t, err, fetchErr)
				}
				if tc.destinationDir {
					require.True(t, gen.IsFilePath(err), "file error classification must survive command wrapping")
					entries, readErr := os.ReadDir(destination)
					require.NoError(t, readErr)
					require.Empty(t, entries)
				} else {
					got, readErr := os.ReadFile(destination) // #nosec G304 -- fixture path under t.TempDir
					require.NoError(t, readErr)
					require.Equal(t, original, string(got), "failed gathering must not publish a partial release map")
				}
			} else {
				require.NoError(t, err)
				got, readErr := os.ReadFile(destination) // #nosec G304 -- fixture path under t.TempDir
				require.NoError(t, readErr)
				want := `{"5000.0.0":{"eu-west-1":"ami-global-5000","cn-northwest-1":"ami-china"},"5001.0.0":{"eu-west-1":"ami-global-5001","cn-northwest-1":"ami-china"}}`
				if tc.keepExisting {
					want = `{"3000.0.0":{"eu-west-1":"ami-unlisted"},"5000.0.0":{"eu-west-1":"ami-kept"},"5001.0.0":{"eu-west-1":"ami-global-5001","cn-northwest-1":"ami-china"}}`
				}
				require.JSONEq(t, want, string(got))
			}

			wantGlobal := []string{"https://beta.release.ami-fixture.invalid/arm64-usr/"}
			wantChina := []string{}
			if !tc.keepExisting {
				wantGlobal = append(wantGlobal, "https://beta.release.ami-fixture.invalid/arm64-usr/5000.0.0/flatcar_production_ami_all.json")
				wantChina = append(wantChina, "/ami-fixture-bucket/beta/arm64-usr/5000.0.0.json")
			}
			wantGlobal = append(wantGlobal, "https://beta.release.ami-fixture.invalid/arm64-usr/5001.0.0/flatcar_production_ami_all.json")
			if !tc.globalFailure {
				wantChina = append(wantChina, "/ami-fixture-bucket/beta/arm64-usr/5001.0.0.json")
			}
			require.Equal(t, wantGlobal, globalRequests)
			mu.Lock()
			gotChina := append([]string{}, chinaRequests...)
			mu.Unlock()
			require.Equal(t, wantChina, gotChina)
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func isolateAWS(t *testing.T, endpoint string) {
	t.Helper()
	configDir := t.TempDir()
	configFile := filepath.Join(configDir, "config")
	require.NoError(t, os.WriteFile(configFile, []byte("[profile ami-fixture]\n"), 0o600))
	credentialsFile := filepath.Join(configDir, "credentials")
	require.NoError(t, os.WriteFile(credentialsFile, nil, 0o600))
	for key, value := range map[string]string{
		"AWS_CONFIG_FILE": configFile, "AWS_SHARED_CREDENTIALS_FILE": credentialsFile,
		"AWS_PROFILE": "ami-fixture", "AWS_DEFAULT_PROFILE": "ami-fixture", "AWS_CA_BUNDLE": "",
		"AWS_ACCESS_KEY_ID": "synthetic-env-access-key", "AWS_SECRET_ACCESS_KEY": "synthetic-env-secret-key", "AWS_SESSION_TOKEN": "",
		"AWS_ENDPOINT_URL": endpoint, "AWS_ENDPOINT_URL_S3": endpoint,
		"AWS_IGNORE_CONFIGURED_ENDPOINT_URLS": "false", "AWS_EC2_METADATA_DISABLED": "true",
		"AWS_MAX_ATTEMPTS": "1", "AWS_RETRY_MODE": "standard",
	} {
		t.Setenv(key, value)
	}
}
