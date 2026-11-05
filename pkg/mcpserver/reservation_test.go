package mcpserver_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/giantswarm/devctl/v8/internal/gittest"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/mcpserver"
)

const (
	gitopsRepository = "giantswarm/giantswarm-management-clusters"
	gitopsCluster    = "graveler"
	gitopsApp        = "hello-world"
)

var bob = mcpserver.Caller{Login: "bob", ID: 4712}

// gitopsFiles are a GitOps repository with one management cluster enabled
// for reservations and one collection app, its collection base in the same
// tree so a render needs no network.
var gitopsFiles = map[string]string{
	"bases/collections/demo/kustomization.yaml": `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - hello-world.yaml
`,
	"bases/collections/demo/hello-world.yaml": `---
apiVersion: source.toolkit.fluxcd.io/v1
kind: OCIRepository
metadata:
  name: hello-world
  namespace: giantswarm
spec:
  interval: 10m
  provider: generic
  ref:
    semver: x.x.x
  url: oci://gsoci.azurecr.io/charts/giantswarm/hello-world
---
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: hello-world
  namespace: giantswarm
spec:
  chartRef:
    kind: OCIRepository
    name: hello-world
    namespace: giantswarm
  interval: 5m
  releaseName: hello-world
  targetNamespace: giantswarm
`,
	"management-clusters/graveler/kustomization.yaml": `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - flux-collections.yaml
  - configmap-reservations.yaml
`,
	"management-clusters/graveler/flux-collections.yaml": `apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: collections
  namespace: flux-giantswarm
spec:
  interval: 10m
  path: management-clusters/graveler/collections
  prune: true
  sourceRef:
    kind: GitRepository
    name: management-clusters-fleet
`,
	"management-clusters/graveler/collections/kustomization.yaml": `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ../../../bases/collections/demo
`,
	"management-clusters/graveler/configmap-reservations.yaml": `apiVersion: v1
kind: ConfigMap
metadata:
  name: reservations
  namespace: giantswarm
  annotations:
    reservations.giantswarm.io/max-duration: 7d
data: {}
`,
}

// gitops is a GitOps repository served from a bare repository on disk, the
// way GitHub serves it, and the directory the server clones it into.
type gitops struct {
	base   string
	bare   string
	clones string
}

func newGitOps(t *testing.T) *gitops {
	t.Helper()
	root := t.TempDir()
	g := &gitops{base: "file://" + root, bare: filepath.Join(root, gitopsRepository+".git"), clones: t.TempDir()}
	if err := os.MkdirAll(g.bare, 0o755); err != nil { //nolint:gosec // a test fixture
		t.Fatal(err)
	}
	gittest.RunGit(t, g.bare, "init", "--quiet", "--bare", "--initial-branch=main")
	work := t.TempDir()
	gittest.RunGit(t, work, "init", "--quiet", "--initial-branch=main")
	for name, content := range gitopsFiles {
		path := filepath.Join(work, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // a test fixture
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec // a test fixture
			t.Fatal(err)
		}
	}
	gittest.RunGit(t, work, "add", ".")
	gittest.RunGit(t, work, "commit", "--quiet", "-m", "fixture")
	gittest.RunGit(t, work, "push", "--quiet", g.bare, "main")
	return g
}

// head is the subject and author of main's tip.
func (g *gitops) head(t *testing.T) string {
	t.Helper()
	return gittest.GitOutput(t, g.bare, "log", "-1", "--format=%an <%ae> %s", "main")
}

// connectPerson connects to a server cloning g as the person caller.
func (g *gitops) connectPerson(t *testing.T, caller mcpserver.Caller) *client.Client {
	t.Helper()
	c, err := connectAs(t, mcpserver.Config{
		Credentials: mcpserver.MusterGrant{App: app, Users: &users{caller: caller}, Installations: &installed{owners: []string{"giantswarm"}}},
		Checkouts:   mcpserver.GitCheckouts{BaseURL: g.base, Dir: g.clones},
	}, map[string]string{mcpserver.GrantHeaderPrefix + app: "ghu_" + caller.Login})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// assertNoClone fails when a clone outlived its call.
func (g *gitops) assertNoClone(t *testing.T) {
	t.Helper()
	entries, err := os.ReadDir(g.clones)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("clones outlived their calls: %v", entries)
	}
}

func reserveArgs() map[string]any {
	return map[string]any{"cluster": gitopsCluster, "app": gitopsApp, "branch": "fix/crash", "pullRequest": "giantswarm/hello-world#7", "duration": "4h"}
}

// TestReservationAsThePerson: a person reserves an app, reservation_list
// names them its holder, and reservation_release returns it to its release;
// each push is a commit of theirs, and no clone outlives its call.
func TestReservationAsThePerson(t *testing.T) {
	g := newGitOps(t)
	c := g.connectPerson(t, alice)

	_, doc := call(t, c, mcpserver.ToolReservationReserve, reserveArgs(), nil)
	assertVerdict(t, doc, agentcli.VerdictGreen, agentcli.ExitOK)
	if doc["user"] != "alice" || doc["app"] != gitopsApp || doc["commit"] == "" || doc["repository"] != gitopsRepository {
		t.Errorf("reserve answered %v", doc)
	}
	if got, want := g.head(t), "alice <4711+alice@users.noreply.github.com> reserve hello-world on graveler for alice"; !strings.HasPrefix(got, want) {
		t.Errorf("main's tip is %q, want %q…", got, want)
	}
	g.assertNoClone(t)

	_, doc = call(t, c, mcpserver.ToolReservationList, map[string]any{"cluster": gitopsCluster}, nil)
	assertVerdict(t, doc, agentcli.VerdictGreen, agentcli.ExitOK)
	reservations := anySlice(doc["reservations"])
	if len(reservations) != 1 {
		t.Fatalf("list answered %v, want one reservation", doc["reservations"])
	}
	entry := reservations[0].(map[string]any)
	if entry["user"] != "alice" || entry["app"] != gitopsApp || entry["branch"] != "fix/crash" || entry["status"] != "active" {
		t.Errorf("list's reservation is %v, want alice's of hello-world, active", entry)
	}

	_, doc = call(t, c, mcpserver.ToolReservationRelease, map[string]any{"cluster": gitopsCluster, "app": gitopsApp}, nil)
	assertVerdict(t, doc, agentcli.VerdictGreen, agentcli.ExitOK)
	if !strings.HasPrefix(g.head(t), "alice <4711+alice@users.noreply.github.com> ") {
		t.Errorf("the release is not alice's commit: %s", g.head(t))
	}
	_, doc = call(t, c, mcpserver.ToolReservationList, map[string]any{"cluster": gitopsCluster}, nil)
	if n := len(anySlice(doc["reservations"])); n != 0 {
		t.Errorf("after the release list answered %d reservations", n)
	}
	g.assertNoClone(t)
}

// TestReservationOfAnotherHolderRefused: a second person's reserve of the
// same app is refused naming the first holder, the branch and the end, as
// the CLI refuses; nothing is pushed.
func TestReservationOfAnotherHolderRefused(t *testing.T) {
	g := newGitOps(t)
	_, doc := call(t, g.connectPerson(t, alice), mcpserver.ToolReservationReserve, reserveArgs(), nil)
	assertVerdict(t, doc, agentcli.VerdictGreen, agentcli.ExitOK)
	until := stringOf(doc["until"])
	tip := g.head(t)

	args := reserveArgs()
	args["branch"], args["pullRequest"] = "feat/other", "giantswarm/hello-world#8"
	_, doc = call(t, g.connectPerson(t, bob), mcpserver.ToolReservationReserve, args, nil)
	assertVerdict(t, doc, agentcli.VerdictRefused, agentcli.ExitRefused)
	reason := stringOf(doc["reason"])
	for _, want := range []string{"alice", "fix/crash", until} {
		if !strings.Contains(reason, want) {
			t.Errorf("the refusal %q does not name %q", reason, want)
		}
	}
	if g.head(t) != tip {
		t.Errorf("the refusal pushed: %s", g.head(t))
	}
	g.assertNoClone(t)
}

// TestReservationExtendTakesOver: reservation_extend makes the caller the
// holder of what the pull request holds, as the CLI's --user does.
func TestReservationExtendTakesOver(t *testing.T) {
	g := newGitOps(t)
	_, doc := call(t, g.connectPerson(t, alice), mcpserver.ToolReservationReserve, reserveArgs(), nil)
	assertVerdict(t, doc, agentcli.VerdictGreen, agentcli.ExitOK)

	c := g.connectPerson(t, bob)
	_, doc = call(t, c, mcpserver.ToolReservationExtend, map[string]any{"pullRequest": "giantswarm/hello-world#7"}, nil)
	assertVerdict(t, doc, agentcli.VerdictGreen, agentcli.ExitOK)
	if n := len(anySlice(doc["extended"])); n != 1 {
		t.Errorf("extend answered %v, want one extension", doc["extended"])
	}
	_, doc = call(t, c, mcpserver.ToolReservationList, map[string]any{"cluster": gitopsCluster}, nil)
	if entry := anySlice(doc["reservations"])[0].(map[string]any); entry["user"] != "bob" {
		t.Errorf("after bob's extend the holder is %v", entry["user"])
	}

	_, doc = call(t, c, mcpserver.ToolReservationRelease, map[string]any{"pullRequest": "giantswarm/hello-world#7"}, nil)
	assertVerdict(t, doc, agentcli.VerdictGreen, agentcli.ExitOK)
	if n := len(anySlice(doc["released"])); n != 1 {
		t.Errorf("release by pull request answered %v, want one release", doc["released"])
	}
	g.assertNoClone(t)
}

// TestReservationDryRunPushesNothing: a dry run answers its commit as a
// patch and leaves main as it was.
func TestReservationDryRunPushesNothing(t *testing.T) {
	g := newGitOps(t)
	tip := g.head(t)
	args := reserveArgs()
	args["dryRun"] = true
	_, doc := call(t, g.connectPerson(t, alice), mcpserver.ToolReservationReserve, args, nil)
	assertVerdict(t, doc, agentcli.VerdictGreen, agentcli.ExitOK)
	if doc["dryRun"] != true || !strings.Contains(stringOf(doc["patch"]), "configmap-reservations.yaml") {
		t.Errorf("the dry run answered no patch: %v", doc)
	}
	if g.head(t) != tip {
		t.Errorf("the dry run pushed: %s", g.head(t))
	}
	g.assertNoClone(t)
}

// TestReservationUsage: wrong arguments are refused before any clone, and a
// missing reservation or cluster is not_applicable.
func TestReservationUsage(t *testing.T) {
	g := newGitOps(t)
	c := g.connectPerson(t, alice)
	for name, tt := range map[string]struct {
		tool    string
		args    map[string]any
		verdict agentcli.Verdict
		exit    int
	}{
		"cluster as a path":    {mcpserver.ToolReservationList, map[string]any{"cluster": "../graveler"}, agentcli.VerdictUsage, agentcli.ExitUsage},
		"branch with a quote":  {mcpserver.ToolReservationReserve, map[string]any{"cluster": gitopsCluster, "app": gitopsApp, "branch": `a"b`}, agentcli.VerdictUsage, agentcli.ExitUsage},
		"duration in words":    {mcpserver.ToolReservationReserve, map[string]any{"cluster": gitopsCluster, "app": gitopsApp, "branch": "b", "duration": "4 hours"}, agentcli.VerdictUsage, agentcli.ExitUsage},
		"release without form": {mcpserver.ToolReservationRelease, map[string]any{"app": gitopsApp}, agentcli.VerdictUsage, agentcli.ExitUsage},
		"repository not one":   {mcpserver.ToolReservationList, map[string]any{"cluster": gitopsCluster, "repository": "x"}, agentcli.VerdictUsage, agentcli.ExitUsage},
		"unknown cluster":      {mcpserver.ToolReservationList, map[string]any{"cluster": "glean"}, agentcli.VerdictNotApplicable, agentcli.ExitNotApplicable},
		"nothing reserved":     {mcpserver.ToolReservationRelease, map[string]any{"cluster": gitopsCluster, "app": gitopsApp}, agentcli.VerdictNotApplicable, agentcli.ExitNotApplicable},
		"nothing to extend":    {mcpserver.ToolReservationExtend, map[string]any{"pullRequest": "giantswarm/hello-world#9"}, agentcli.VerdictNotApplicable, agentcli.ExitNotApplicable},
	} {
		t.Run(name, func(t *testing.T) {
			_, doc := call(t, c, tt.tool, tt.args, nil)
			assertVerdict(t, doc, tt.verdict, tt.exit)
		})
	}
	g.assertNoClone(t)
}

// TestReservationToolsTakeNoUser: the holder is the person's login, never
// an argument.
func TestReservationToolsTakeNoUser(t *testing.T) {
	c, _ := connect(t, mcpserver.Config{})
	tools, err := c.ListTools(context.Background(), mcp.ListToolsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, tool := range tools.Tools {
		if !strings.HasPrefix(tool.Name, "reservation_") {
			continue
		}
		seen++
		if _, ok := tool.InputSchema.Properties["user"]; ok {
			t.Errorf("%s takes a user argument", tool.Name)
		}
	}
	if seen != 4 {
		t.Errorf("%d reservation tools served, want 4", seen)
	}
}
