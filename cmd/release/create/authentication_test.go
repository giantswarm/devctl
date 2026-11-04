package create

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giantswarm/micrologger"
	"github.com/giantswarm/releases/sdk/api/v1alpha1"
	"github.com/google/go-github/v93/github"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

const (
	syntheticAppToken      = "synthetic-release-app-token"
	syntheticOverrideToken = "synthetic-release-override-token"
)

// Run the public command through the real release generator. Both the upstream
// Kubernetes lookup and the giantswarm app lookup must use the selected login.
func TestCreateUsesSelectedGitHubIdentity(t *testing.T) {
	if _, err := exec.LookPath("diff"); err != nil {
		t.Skip("the release generator requires the diff executable")
	}
	for _, override := range []bool{false, true} {
		name := "App login"
		if override {
			name = "environment override"
		}
		t.Run(name, func(t *testing.T) {
			cmd, provider, stderr, _ := authenticatedCreateFixture(t, override)
			selected := syntheticAppToken
			if override {
				selected = syntheticOverrideToken
			}
			reads := mockReleaseReads(t, selected, http.StatusOK)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("create failed: %v", err)
			}
			if len(*reads) != 2 || (*reads)[0] != "/repos/kubernetes/kubernetes/releases" || !strings.HasPrefix((*reads)[1], "/repos/giantswarm/") {
				t.Fatalf("expected upstream and giantswarm reads, got %v", *reads)
			}
			var release v1alpha1.Release
			if err := yaml.Unmarshal(readCreateFile(t, provider, "v30.1.0/release.yaml"), &release); err != nil {
				t.Fatal(err)
			}
			if release.Name != "aws-30.1.0" || len(release.Spec.Apps) != 1 || release.Spec.Apps[0].Version != "1.0.0" || len(release.Spec.Components) != 1 || release.Spec.Components[0].Version != "1.30.0" {
				t.Errorf("unexpected generated release: name=%s apps=%v components=%v", release.Name, release.Spec.Apps, release.Spec.Components)
			}
			var index struct {
				Releases []struct{ Version string }
			}
			if err := json.Unmarshal(readCreateFile(t, provider, "releases.json"), &index); err != nil {
				t.Fatal(err)
			}
			if len(index.Releases) != 1 || index.Releases[0].Version != "30.1.0" {
				t.Errorf("release was not registered: %+v", index)
			}
			var kustomization struct{ Resources []string }
			if err := yaml.Unmarshal(readCreateFile(t, provider, "kustomization.yaml"), &kustomization); err != nil {
				t.Fatal(err)
			}
			if strings.Join(kustomization.Resources, ",") != "v30.0.0,v30.1.0" {
				t.Errorf("release was not added to kustomization: %v", kustomization.Resources)
			}
			if !strings.Contains(string(readCreateFile(t, provider, "v30.1.0/README.md")), "CAPA") {
				t.Error("--provider aws did not produce CAPA release notes")
			}
			if _, err := os.Stat(filepath.Join(provider, "v30.1.0/keep.txt")); !os.IsNotExist(err) {
				t.Errorf("--overwrite did not replace the existing target (stat error: %v)", err)
			}
			assertNoCreateTokenOutput(t, stderr.String())
		})
	}
}

// Lookup failures must retain the GitHub status and leave release files intact.
// Only a 404 under the App login gets the App-scope hint; an override is never
// retried as the stored App identity, even after an authentication refusal.
func TestCreateGitHubRefusalsPreserveRelease(t *testing.T) {
	for _, tc := range []struct {
		name     string
		override bool
		status   int
		hint     bool
	}{
		{name: "App not found", status: http.StatusNotFound, hint: true},
		{name: "override not found", override: true, status: http.StatusNotFound},
		{name: "App forbidden", status: http.StatusForbidden},
		{name: "override unauthorized", override: true, status: http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd, _, stderr, unchanged := authenticatedCreateFixture(t, tc.override)
			selected := syntheticAppToken
			if tc.override {
				selected = syntheticOverrideToken
			}
			reads := mockReleaseReads(t, selected, tc.status)
			err := cmd.Execute()
			var response *github.ErrorResponse
			if !errors.As(err, &response) || response.Response == nil || response.Response.StatusCode != tc.status {
				t.Fatalf("create returned %v, want GitHub status %d", err, tc.status)
			}
			if got := strings.Contains(err.Error(), "devctl GitHub App login reaches"); got != tc.hint {
				t.Errorf("App-scope hint present=%t, want %t: %v", got, tc.hint, err)
			}
			// A 404 may try the app's alternate repository name. Other refusals
			// must stop immediately after the upstream and first app read.
			if len(*reads) < 2 || len(*reads) > 3 || (tc.status != http.StatusNotFound && len(*reads) != 2) {
				t.Errorf("unexpected number of GitHub reads: %v", *reads)
			}
			unchanged()
			assertNoCreateTokenOutput(t, stderr.String()+err.Error())
		})
	}
}

func TestCreateCIWithoutOverrideStopsBeforeReleaseReads(t *testing.T) {
	cmd, _, _, unchanged := authenticatedCreateFixture(t, false)
	t.Setenv("CI", "1")
	reads := mockReleaseReads(t, syntheticAppToken, http.StatusOK)
	if err := cmd.Execute(); !errors.Is(err, authstore.ErrAuthRequired) {
		t.Fatalf("create returned %v, want authentication required despite the stored App login", err)
	}
	if len(*reads) != 0 {
		t.Errorf("unauthenticated create read GitHub: %v", *reads)
	}
	unchanged()
}

type createAuthTransport func(*http.Request) (*http.Response, error)

func (f createAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// Intercept the default transport: no request can escape to a live endpoint.
func mockReleaseReads(t *testing.T, selected string, appStatus int) *[]string {
	t.Helper()
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	var reads []string
	http.DefaultTransport = createAuthTransport(func(req *http.Request) (*http.Response, error) {
		reads = append(reads, req.URL.Path)
		if req.Header.Get("Authorization") != "Bearer "+selected {
			t.Error("release lookup did not use the selected GitHub identity")
		}
		if req.Method != http.MethodGet || req.URL.Scheme != "https" || req.URL.Host != "api.github.com" {
			t.Error("unexpected release request")
			return nil, errors.New("unexpected offline release request")
		}
		status, body := http.StatusOK, ""
		switch req.URL.Path {
		case "/repos/kubernetes/kubernetes/releases":
			body = `[{"name":"v1.30.0","tag_name":"v1.30.0"}]`
		case "/repos/giantswarm/cert-manager/releases", "/repos/giantswarm/cert-manager-app/releases":
			status = appStatus
			body = `[{"name":"v1.0.0","tag_name":"v1.0.0"}]`
			if status != http.StatusOK {
				body = `{"message":"` + http.StatusText(status) + `"}`
			}
		default:
			t.Error("unexpected release lookup path: " + req.URL.Path)
			return nil, errors.New("unexpected offline release lookup")
		}
		return &http.Response{
			StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(body)), Request: req,
		}, nil
	})
	return &reads
}

func authenticatedCreateFixture(t *testing.T, override bool) (*cobra.Command, string, *bytes.Buffer, func()) {
	t.Helper()
	t.Setenv("CI", "")
	t.Setenv(agentcli.EnvTimeScale, "1")
	for _, name := range authstore.GitHubEnvVars {
		t.Setenv(name, "")
	}
	if override {
		t.Setenv("DEVCTL_GITHUB_TOKEN", syntheticOverrideToken)
	}
	store := &authstore.FileStore{Path: filepath.Join(t.TempDir(), "keyring.json")}
	t.Setenv(agentcli.EnvKeyringFile, store.Path)
	if err := store.Set(authstore.UserGitHub, authstore.Record{Token: syntheticAppToken}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	provider := filepath.Join(dir, "capa")
	files := map[string]string{
		"v30.0.0/release.yaml": "apiVersion: release.giantswarm.io/v1alpha1\nkind: Release\nmetadata:\n  name: aws-30.0.0\nspec:\n  state: active\n  components:\n  - name: kubernetes\n    version: 1.30.0\n  apps:\n  - name: cert-manager\n    version: 1.0.0\n",
		"v30.1.0/keep.txt":     "existing release content\n",
		"kustomization.yaml":   "resources:\n- v30.0.0\n",
		"releases.json":        "{\"releases\":[]}\n",
	}
	for name, content := range files {
		path := filepath.Join(provider, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	logger, err := micrologger.New(micrologger.Config{})
	if err != nil {
		t.Fatal(err)
	}
	stderr := &bytes.Buffer{}
	cmd, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: stderr})
	if err != nil {
		t.Fatal(err)
	}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--name", "v30.1.0", "--base", "v30.0.0", "--provider", "aws", "--releases", dir, "--bumpall", "--yes", "--overwrite"})
	return cmd, provider, stderr, func() {
		t.Helper()
		for name, want := range files {
			if string(readCreateFile(t, provider, name)) != want {
				t.Errorf("failed create changed %s", name)
			}
		}
		if _, err := os.Stat(filepath.Join(provider, "v30.1.0/release.yaml")); !os.IsNotExist(err) {
			t.Errorf("failed create wrote release.yaml (stat error: %v)", err)
		}
	}
}

func readCreateFile(t *testing.T, provider, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(provider, name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertNoCreateTokenOutput(t *testing.T, text string) {
	t.Helper()
	if strings.Contains(text, syntheticAppToken) || strings.Contains(text, syntheticOverrideToken) {
		t.Error("create disclosed a synthetic token")
	}
}
