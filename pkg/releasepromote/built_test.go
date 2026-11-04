package releasepromote

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	registrymock "github.com/giantswarm/devctl/v8/e2e/mock/registry"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
	"github.com/giantswarm/devctl/v8/pkg/releasewait/releasewaittest"
)

// verifiedBuild is the explicit successful artifact verdict in promotion
// tests that exercise other contracts.
func verifiedBuild(context.Context, string, string, string) error { return nil }

func TestPromoteBuildVerification(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     githubclient.CombinedStatus
		missing    string
		dryRun     bool
		noVerifier bool
		wantState  string
		wantCheck  bool
	}{
		{name: "missing status", status: githubclient.CombinedStatus{State: "pending"}, missing: "no commit status", wantState: StateNotBuilt},
		{name: "empty successful response", status: githubclient.CombinedStatus{State: "success"}, missing: "no commit status", wantState: StateNotBuilt},
		{name: "missing image", status: builtStatus, missing: "image gsoci.azurecr.io/giantswarm/renamed:1.3.0-rc.2", wantState: StateNotBuilt, wantCheck: true},
		{name: "missing chart in dry run", status: builtStatus, missing: "chart gsoci.azurecr.io/charts/giantswarm/chart:1.3.0-rc.2", dryRun: true, wantState: StateNotBuilt, wantCheck: true},
		{name: "missing verifier", status: builtStatus, noVerifier: true, missing: "no build verifier", wantState: StateNotBuilt},
		{name: "built", status: builtStatus, wantState: StateDispatched, wantCheck: true},
		{name: "built dry run", status: builtStatus, dryRun: true, wantState: StateWouldDispatch, wantCheck: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gh := &fakeGitHub{repositories: map[string]fakeRepository{"giantswarm/app": {
				branch: "main", releases: []githubclient.Release{candidate("v1.3.0-rc.2")}, status: tc.status,
			}}}
			var checked []string
			config := Config{GitHub: gh, DryRun: tc.dryRun, CheckBuilt: func(_ context.Context, owner, repo, tag string) error {
				checked = append(checked, owner+"/"+repo+"@"+tag)
				if tc.missing != "" {
					return errors.New(tc.missing)
				}
				return nil
			}}
			if tc.noVerifier {
				config.CheckBuilt = nil
			}
			result := NewResult("")
			err := Promote(t.Context(), config, []string{"giantswarm/app"}, &result)
			require.Equal(t, tc.wantState, result.Repositories[0].State)
			if tc.wantCheck {
				require.Equal(t, []string{"giantswarm/app@v1.3.0-rc.2"}, checked)
			} else {
				require.Empty(t, checked)
			}
			if tc.wantState == StateNotBuilt {
				require.Equal(t, agentcli.ExitRed, agentcli.Exit(err))
				require.Contains(t, result.Repositories[0].Message, tc.missing)
			} else {
				require.NoError(t, err)
			}
			if tc.wantState == StateDispatched {
				require.Len(t, gh.dispatches, 1)
			} else {
				require.Empty(t, gh.dispatches)
			}
		})
	}
}

func TestPromoteNewestBuiltVerifiesArtifacts(t *testing.T) {
	gh := &fakeGitHub{repositories: map[string]fakeRepository{"giantswarm/app": {
		branch: "main", releases: streamReleases, statuses: streamStatuses,
	}}}
	var checked []string
	result := NewResult("")
	err := Promote(t.Context(), Config{GitHub: gh, CheckBuilt: func(_ context.Context, _, _, tag string) error {
		checked = append(checked, tag)
		if tag == "v1.2.3-rc.14" {
			return errors.New("chart missing")
		}
		return nil
	}}, []string{"giantswarm/app"}, &result)
	require.Equal(t, agentcli.ExitRed, agentcli.Exit(err))
	require.Equal(t, "v1.2.3-rc.13", result.Repositories[0].NewestBuilt)
	require.Equal(t, []string{"v1.2.3-rc.14", "v1.2.3-rc.13"}, checked)
	require.Empty(t, gh.dispatches)
}

// Exercise the real release waiter, artifact derivation, and OCI HEAD/GET
// probing. Only GitHub/CircleCI metadata and the loopback registry are synthetic.
func TestCheckBuiltArtifacts(t *testing.T) {
	const tag = "v1.2.3-rc.1"
	for _, tc := range []struct {
		name, missing, workflow, want string
		unverified                    bool
	}{
		{name: "published", workflow: "success"},
		{name: "missing image", missing: "image", workflow: "success", want: "giantswarm/app:1.2.3-rc.1"},
		{name: "missing renamed chart", missing: "chart", workflow: "success", want: "charts/giantswarm/published-chart:1.2.3-rc.1"},
		{name: "pipeline still running", workflow: "running", want: "did not finish"},
		{name: "pipeline failed", workflow: "failed", want: "tag pipeline"},
		{name: "unverified tag", workflow: "success", unverified: true, want: "no verified CircleCI tag pipeline"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			routes := sequence.Routes{}
			if tc.missing != "image" {
				routes["HEAD /v2/giantswarm/app/manifests/1.2.3-rc.1"] = []sequence.Response{{Status: 200}}
			}
			if tc.missing != "chart" {
				routes["HEAD /v2/charts/giantswarm/published-chart/manifests/1.2.3-rc.1"] = []sequence.Response{{Status: 200}}
			}
			registry, err := registrymock.Start(routes, true)
			require.NoError(t, err)
			t.Cleanup(registry.Close)
			endpoints := agentcli.DefaultEndpoints()
			endpoints.RegistryPublic, endpoints.RegistryInsecure = registry.Host(), true
			sources, err := releasewaittest.GeneratedRelease("giantswarm", "app", &releasewaittest.Registry{}, tc.workflow)
			require.NoError(t, err)
			gh := sources.GitHub.(*releasewaittest.GitHub)
			gh.Tags = map[string]string{tag: releasewaittest.ReleaseSHA}
			gh.Files["helm/app/Chart.yaml"] = "apiVersion: v2\nname: published-chart\nversion: 1.2.3-rc.1\n"
			cci, err := sources.CircleCI(t.Context())
			require.NoError(t, err)
			cci.(*releasewaittest.CircleCI).Pipeline.VCS.Tag = tag
			if tc.unverified {
				cci.(*releasewaittest.CircleCI).Pipeline.ID = ""
			}
			sources.Registry = releasewait.RegistryProber{Endpoints: endpoints}
			err = checkBuilt(t.Context(), sources, endpoints, agentcli.NewVirtualClock(time.Unix(0, 0)), "giantswarm", "app", tag)
			if tc.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.want)
			}
			if tc.missing != "" {
				found := false
				for _, request := range registry.Requests() {
					if request.Method == "GET" && strings.Contains(request.Path, "manifests/1.2.3-rc.1") {
						found = true
					}
				}
				require.True(t, found, "missing HEAD must be confirmed with a manifest GET")
			}
		})
	}
}
