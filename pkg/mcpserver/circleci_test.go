package mcpserver_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/go-github/v93/github"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/mcpserver"
	"github.com/giantswarm/devctl/v8/pkg/prwait/prwaittest"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
	"github.com/giantswarm/devctl/v8/pkg/releasewait/releasewaittest"
)

// generatedCircleCI are devctl's own generated CircleCI configuration files:
// on a pull request's branch, build-image runs behind go-build.
func generatedCircleCI(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{}
	for _, path := range []string{".circleci/config.yml", ".circleci/workflows.yml"} {
		content, err := fs.ReadFile(os.DirFS(filepath.Join("..", "..")), path)
		if err != nil {
			t.Fatal(err)
		}
		files[path] = string(content)
	}
	return files
}

func circleStatus(job, state string) *github.RepoStatus {
	return prwaittest.Status("ci/circleci: "+job, state)
}

// TestPRWaitJobBehindRequiresKeepsItPending: judged from the commit
// statuses alone, a head whose finished jobs all passed is not green while a
// job behind requires: has posted nothing: devctl's build-image, which
// requires go-build, posts its first status only once go-build finished.
func TestPRWaitJobBehindRequiresKeepsItPending(t *testing.T) {
	beforeBuildImage := prwaittest.Head{Statuses: []*github.RepoStatus{circleStatus("setup", "success"), circleStatus("go-build", "success")}}
	tests := []struct {
		name    string
		heads   []prwaittest.Head
		verdict agentcli.Verdict
		exit    int
	}{
		{
			name: "green once the job behind requires: posted and passed",
			heads: []prwaittest.Head{
				beforeBuildImage,
				{Statuses: []*github.RepoStatus{circleStatus("setup", "success"), circleStatus("go-build", "success"), circleStatus("build-image", "pending")}},
				{Statuses: []*github.RepoStatus{circleStatus("setup", "success"), circleStatus("go-build", "success"), circleStatus("build-image", "success")}},
			},
			verdict: agentcli.VerdictGreen,
		},
		{
			name:    "never green while it has posted nothing",
			heads:   []prwaittest.Head{beforeBuildImage},
			verdict: agentcli.VerdictTimeout,
			exit:    agentcli.ExitTimeout,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gh := &prwaittest.GitHub{Files: generatedCircleCI(t), Heads: tt.heads}
			c, _ := connect(t, mcpserver.Config{Credentials: person("ghu_person"), Sources: &sources{pr: gh}, Clock: virtual()})
			_, doc := call(t, c, mcpserver.ToolPRWait, map[string]any{"repository": "giantswarm/devctl", "number": 7, "timeout": "10m"}, "wait-1")
			assertVerdict(t, doc, tt.verdict, tt.exit)
			if tt.verdict == agentcli.VerdictGreen && gh.Polls() < len(tt.heads) {
				t.Errorf("green after %d polls, before build-image passed at poll %d", gh.Polls(), len(tt.heads))
			}
			if tt.verdict == agentcli.VerdictTimeout && !strings.Contains(stringOf(doc["reason"]), "ci/circleci: build-image (not posted yet)") {
				t.Errorf("the reason does not name the job that posted nothing: %v", doc["reason"])
			}
		})
	}
}

// TestReleaseWaitReadsNoCircleCI: release_wait judges the tag's CI from the
// commit statuses CircleCI posts and never builds a CircleCI client, even
// when its sources offer one; an artifact that is pullable is not the
// release while a tag job has posted nothing.
func TestReleaseWaitReadsNoCircleCI(t *testing.T) {
	setup := githubclient.CommitStatus{Context: "ci/circleci: setup", State: "success", TargetURL: "https://circleci.com/gh/giantswarm/devctl/100"}
	push := func(state string) githubclient.CommitStatus {
		return githubclient.CommitStatus{Context: "ci/circleci: push-to-registries-release", State: state, TargetURL: "https://circleci.com/gh/giantswarm/devctl/101"}
	}
	tests := []struct {
		name    string
		reads   [][]githubclient.CommitStatus
		verdict agentcli.Verdict
		exit    int
		reason  string
	}{
		{
			name:    "available once the tag job posted and passed",
			reads:   [][]githubclient.CommitStatus{{setup}, {push("pending"), setup}, {push("success"), setup}},
			verdict: agentcli.VerdictAvailable,
		},
		{
			name:    "never available while the tag job posted nothing",
			reads:   [][]githubclient.CommitStatus{{setup}},
			verdict: agentcli.VerdictTimeout,
			exit:    agentcli.ExitTimeout,
			reason:  "push-to-registries-release (not posted yet)",
		},
		{
			name:    "the tag job failed",
			reads:   [][]githubclient.CommitStatus{{push("failure"), setup}},
			verdict: agentcli.VerdictCIFailed,
			exit:    agentcli.ExitRed,
			reason:  "push-to-registries-release",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release, err := releasewaittest.GeneratedRelease("giantswarm", "devctl", &releasewaittest.Registry{}, "success")
			if err != nil {
				t.Fatal(err)
			}
			release.GitHub.(*releasewaittest.GitHub).StatusReads = tt.reads
			var circleci atomic.Int32
			offered := release.CircleCI
			release.CircleCI = func(ctx context.Context) (releasewait.CircleCI, error) {
				circleci.Add(1)
				return offered(ctx)
			}
			c, _ := connect(t, mcpserver.Config{Credentials: person("ghu_person"), Sources: &sources{release: release}, Clock: virtual()})
			_, doc := call(t, c, mcpserver.ToolReleaseWait, map[string]any{"repository": "giantswarm/devctl", "version": "v1.2.3", "timeout": "10m"}, "release-1")
			assertVerdict(t, doc, tt.verdict, tt.exit)
			if !strings.Contains(stringOf(doc["reason"]), tt.reason) {
				t.Errorf("reason %q does not name %q", doc["reason"], tt.reason)
			}
			if n := circleci.Load(); n > 0 {
				t.Errorf("the wait built a CircleCI client %d times", n)
			}
		})
	}
}

// TestToolsListServesNoCircleCICommand: the server holds no CircleCI
// credential, so no rerun and no CircleCI job listing is a tool.
func TestToolsListServesNoCircleCICommand(t *testing.T) {
	for _, config := range []mcpserver.Config{{}, {ReleasePromote: true}} {
		c, _ := connect(t, config)
		tools, err := c.ListTools(context.Background(), mcp.ListToolsRequest{})
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, tool := range tools.Tools {
			names = append(names, tool.Name)
			if strings.Contains(tool.Name, "rerun") || tool.Name == "ci_jobs" || strings.HasPrefix(tool.Name, "ci_") {
				t.Errorf("tools/list names %s", tool.Name)
			}
		}
		if !slices.Contains(names, mcpserver.ToolPRWait) || !slices.Contains(names, mcpserver.ToolReleaseWait) {
			t.Errorf("tools/list names %v, without the waits", names)
		}
	}
}
