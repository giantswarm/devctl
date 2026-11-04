package releasewait

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

type publicationReader struct {
	clock             agentcli.Clock
	start             time.Time
	pagesAfter        time.Duration
	repositoryAfter   time.Duration
	repositoryError   error
	repositoryMissing string
	repositoryCalls   int
	pagesCalls        int
	cancel            context.CancelFunc
}

func (r *publicationReader) Lists(context.Context, string, string, string) (bool, error) {
	r.pagesCalls++
	if r.cancel != nil && r.clock.Now().After(r.start) {
		r.cancel()
	}
	return r.pagesAfter >= 0 && r.clock.Now().Sub(r.start) >= r.pagesAfter, nil
}

func (r *publicationReader) RepositoryLists(_ context.Context, _, chart, _ string) (bool, error) {
	r.repositoryCalls++
	return r.repositoryError == nil && r.repositoryAfter >= 0 && chart != r.repositoryMissing && r.clock.Now().Sub(r.start) >= r.repositoryAfter, r.repositoryError
}

func publicationFixture(t *testing.T) fixture {
	t.Helper()
	return fixture{
		entry: generatedEntry(t), catalog: true, timeout: 30 * time.Second,
		github: baseGitHub([]string{"helm"}, []string{"zz_generated.auto_release.yaml"}, []string{"config.yml", "workflows.yml"}),
		circleci: pipelineRoutes([][]map[string]any{{wf("w1", "build", "success", "2026-09-21T10:00:00Z")}},
			map[string][]map[string]any{"w1": {job("push-chart-release", "success")}}),
		registry: sequence.Routes{"HEAD /v2/charts/giantswarm/kserve/manifests/1.2.3": {{Status: http.StatusOK}}},
	}
}

func TestWaitCatalogPagesPublication(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		pagesAfter, repositoryAfter   time.Duration
		parentTimeout                 time.Duration
		repositoryError               error
		cancel, noCatalog             bool
		wantExit, wantRepositoryCalls int
		wantElapsed                   time.Duration
		wantReason                    string
	}{
		{name: "Pages publishes after the ordinary deadline", pagesAfter: time.Minute, wantElapsed: time.Minute, wantRepositoryCalls: 1},
		{name: "repository progress appears on a later poll", pagesAfter: time.Minute, repositoryAfter: 15 * time.Second, wantElapsed: time.Minute, wantRepositoryCalls: 2},
		{name: "Pages never publishes", pagesAfter: -1, wantElapsed: 30*time.Second + CatalogPublishGrace, wantRepositoryCalls: 1, wantExit: agentcli.ExitTimeout, wantReason: "plus the 10m0s Pages publication extension: giantswarm-catalog/kserve 1.2.3 in repository index.yaml, Pages not yet serving"},
		{name: "repository never lists the chart", pagesAfter: -1, repositoryAfter: -1, wantElapsed: 30 * time.Second, wantRepositoryCalls: 2, wantExit: agentcli.ExitTimeout, wantReason: "no repository index entry confirmed"},
		{name: "repository read refused", pagesAfter: -1, repositoryError: errors.New("repository read refused"), wantElapsed: 30 * time.Second, wantRepositoryCalls: 2, wantExit: agentcli.ExitTimeout, wantReason: "repository index could not be checked: repository read refused"},
		{name: "Pages already lists the chart", wantElapsed: 0, wantRepositoryCalls: 0},
		{name: "Pages publishes before the ordinary deadline", pagesAfter: 15 * time.Second, wantElapsed: 15 * time.Second, wantRepositoryCalls: 1},
		{name: "earlier caller deadline", pagesAfter: -1, parentTimeout: 15 * time.Second, wantElapsed: 15 * time.Second, wantRepositoryCalls: 1, wantExit: agentcli.ExitTimeout, wantReason: "in repository index.yaml, Pages not yet serving"},
		{name: "caller ends during extension", pagesAfter: -1, parentTimeout: time.Minute, wantElapsed: time.Minute, wantRepositoryCalls: 1, wantExit: agentcli.ExitTimeout, wantReason: "in repository index.yaml, Pages not yet serving"},
		{name: "caller cancels after progress", pagesAfter: -1, cancel: true, wantElapsed: 15 * time.Second, wantRepositoryCalls: 1, wantExit: agentcli.ExitTimeout, wantReason: "in repository index.yaml, Pages not yet serving"},
		{name: "catalog flag omitted", pagesAfter: -1, noCatalog: true, wantElapsed: 0, wantRepositoryCalls: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			clock := agentcli.NewVirtualClock(start)
			reader := &publicationReader{clock: clock, start: start, pagesAfter: tc.pagesAfter, repositoryAfter: tc.repositoryAfter, repositoryError: tc.repositoryError}
			fx := publicationFixture(t)
			fx.clock, fx.catalogIndex, fx.catalog = &clock, reader, !tc.noCatalog
			if tc.parentTimeout > 0 {
				var cancel context.CancelFunc
				fx.ctx, cancel = clock.Timeout(context.Background(), tc.parentTimeout)
				defer cancel()
			}
			if tc.cancel {
				fx.ctx, reader.cancel = context.WithCancel(context.Background())
				defer reader.cancel()
			}
			result, err := run(t, fx)
			assertExit(t, err, tc.wantExit, tc.wantReason)
			if elapsed := clock.Now().Sub(start); elapsed != tc.wantElapsed {
				t.Errorf("elapsed %s, want %s", elapsed, tc.wantElapsed)
			}
			if reader.repositoryCalls != tc.wantRepositoryCalls {
				t.Errorf("repository reads %d, want %d", reader.repositoryCalls, tc.wantRepositoryCalls)
			}
			if tc.noCatalog && reader.pagesCalls != 0 {
				t.Errorf("read Pages without --catalog")
			}
			if len(result.Artifacts) != 1 || result.Artifacts[0].State != StateAvailable {
				t.Errorf("registry availability was lost: %+v", result.Artifacts)
			}
		})
	}
}

func TestWaitCatalogExtensionStillChecksCI(t *testing.T) {
	start := time.Now()
	clock := agentcli.NewVirtualClock(start)
	fx := publicationFixture(t)
	fx.clock, fx.catalogIndex = &clock, &publicationReader{clock: clock, start: start, pagesAfter: -1}
	fx.circleci = pipelineRoutes([][]map[string]any{
		{wf("w1", "build", "success", "2026-09-21T10:00:00Z")},
		{wf("w1", "build", "success", "2026-09-21T10:00:00Z")},
		{wf("w2", "build", "failed", "2026-09-21T10:01:00Z")},
	}, map[string][]map[string]any{"w2": {job("push-chart-release", "failed")}})
	_, err := run(t, fx)
	assertExit(t, err, agentcli.ExitRed, "tag pipeline of v1.2.3 failed")
	if elapsed := clock.Now().Sub(start); elapsed != fx.timeout {
		t.Errorf("CI failure was not read at the start of the extension: %s", elapsed)
	}
}

func TestWaitCatalogDoesNotExtendPendingStatuses(t *testing.T) {
	start := time.Now()
	clock := agentcli.NewVirtualClock(start)
	fx := publicationFixture(t)
	fx.clock, fx.catalogIndex = &clock, &publicationReader{clock: clock, start: start}
	fx.github = commitStatuses(fx.github, [][3]string{{"integration-test", "pending", "https://example.invalid/job"}})
	_, err := run(t, fx)
	assertExit(t, err, agentcli.ExitTimeout, "pending")
	if elapsed := clock.Now().Sub(start); elapsed != fx.timeout {
		t.Errorf("Pages was already current but a status received an extension: %s", elapsed)
	}
}

func TestCatalogExtensionRequiresEveryMissingChart(t *testing.T) {
	reader := &publicationReader{pagesAfter: -1, repositoryMissing: "second"}
	w := &Waiter{config: Config{CatalogIndex: reader}, progress: agentcli.NewProgress(nil, false)}
	p := &plan{version: "1.2.3", charts: []chartArtifact{{name: "first", catalog: "custom-catalog"}, {name: "second", catalog: "custom-catalog"}}}
	done, err := w.catalogLists(context.Background(), &Result{}, p)
	if err != nil || done || p.catalogReady {
		t.Fatalf("a missing second chart must withhold the extension: done=%v ready=%v err=%v", done, p.catalogReady, err)
	}
	if len(w.catalogWaiting) != 2 || !strings.Contains(w.catalogWaiting[0], "in repository index.yaml") || !strings.Contains(w.catalogWaiting[1], "no repository index entry confirmed") {
		t.Fatalf("missing per-chart evidence: %v", w.catalogWaiting)
	}
	reader.repositoryMissing = ""
	done, err = w.catalogLists(context.Background(), &Result{}, p)
	if err != nil || done || !p.catalogReady || reader.repositoryCalls != 3 {
		t.Fatalf("repository publication must permit waiting, without declaring success: done=%v ready=%v reads=%d err=%v", done, p.catalogReady, reader.repositoryCalls, err)
	}
}

func TestCatalogIndexReadsPagesAndRepositorySeparately(t *testing.T) {
	// A repository index above 1 MB must be requested as raw bytes; GitHub's
	// JSON content response would have no content at this size.
	const entry = "entries:\n  model-manager:\n  - version: 1.16.0-rc.1\n"
	for _, tc := range []struct {
		name                  string
		status                int
		body                  string
		wantListed, wantError bool
	}{
		{name: "large index", status: http.StatusOK, body: "#" + strings.Repeat("x", 1<<20) + "\n" + entry, wantListed: true},
		{name: "other chart or version", status: http.StatusOK, body: "entries:\n  other:\n  - version: 1.16.0-rc.1\n  model-manager:\n  - version: 1.16.0\n"},
		{name: "not found", status: http.StatusNotFound, body: `{"message":"Not Found"}`},
		{name: "refused", status: http.StatusForbidden, body: `{"message":"Forbidden"}`, wantError: true},
		{name: "malformed", status: http.StatusOK, body: "entries: [", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repositoryReads, pagesReads := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Cache-Control") != "no-cache" {
					t.Error("index request must revalidate caches")
				}
				switch r.URL.Path {
				case "/repos/giantswarm/giantswarm-test-catalog/contents/index.yaml":
					repositoryReads++
					if r.Header.Get("Accept") != "application/vnd.github.raw+json" || r.URL.RawQuery != "" {
						t.Error("repository read must request raw content on the default branch")
					}
					w.WriteHeader(tc.status)
					_, _ = fmt.Fprint(w, tc.body)
				case "/giantswarm-test-catalog/index.yaml":
					pagesReads++
					if r.Header.Get("Authorization") != "" {
						t.Error("Pages must be read anonymously")
					}
					w.WriteHeader(http.StatusNotFound)
				default:
					t.Errorf("unexpected request path: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			sources, err := NewSources(agentcli.Endpoints{GitHubAPIURL: server.URL}, nil, Credentials{GitHubToken: "test-fixture-token"})
			if err != nil {
				t.Fatal(err)
			}
			index := sources.CatalogIndex.(CatalogIndex)
			index.BaseURL = server.URL
			listed, err := index.Lists(context.Background(), "giantswarm-test-catalog", "model-manager", "1.16.0-rc.1")
			if listed || err != nil {
				t.Fatalf("Pages 404 must remain pending: listed=%v err=%v", listed, err)
			}
			listed, err = index.RepositoryLists(context.Background(), "giantswarm-test-catalog", "model-manager", "1.16.0-rc.1")
			if listed != tc.wantListed || (err != nil) != tc.wantError || repositoryReads != 1 || pagesReads != 1 {
				t.Fatalf("listed=%v err=%v repository reads=%d Pages reads=%d", listed, err, repositoryReads, pagesReads)
			}
		})
	}
}
