package workflows

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The mctl workflows read PR links from init PR comments and act on them with an
// App token that reaches every repository of the org (giantswarm/giantswarm#39465,
// #39466). These tests run the rendered github-script bodies in node against a
// fake GitHub API and pin who may write the markers and which PRs they may name.

// markerHarness runs a github-script body with stubbed github, context and core,
// and prints the recorded calls and outputs as JSON.
const markerHarness = `
const fs = require('fs');
const sc = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
const body = fs.readFileSync(process.argv[3], 'utf8');
const calls = [];
const outputs = {};
const key = a => a.owner + '/' + a.repo + '#' + a.pull_number;
const notFound = () => Object.assign(new Error('Not Found'), { status: 404 });
// No waiting in tests.
global.setTimeout = fn => { fn(); return 0; };
// A queued branch update lands after this many reads of the PR.
const pending = {};
const github = {
  paginate: async (fn, args) => (await fn(args)).data,
  graphql: async (q, v) => {
    if (!q.includes('expectedHeadOid: $sha')) throw new Error('auto-merge without expectedHeadOid');
    calls.push({ op: 'automerge', target: v.id, sha: v.sha });
    return {};
  },
  rest: {
    issues: {
      listComments: async () => ({ data: sc.comments }),
      createComment: async a => { calls.push({ op: 'comment', target: a.body }); return { data: {} }; },
      updateComment: async () => ({ data: {} }),
    },
    reactions: { createForIssueComment: async () => ({ data: {} }) },
    repos: {
      createCommitStatus: async a => { calls.push({ op: 'status', target: a.state + ': ' + a.description }); return { data: {} }; },
      getCollaboratorPermissionLevel: async a => {
        const p = (sc.permissions || {})[a.username];
        if (p === 'error') throw Object.assign(new Error('Bad Gateway'), { status: 502 });
        if (!p) throw notFound();
        return { data: { permission: p } };
      },
      // A behind branch updated by GitHub: sc.updates names the merge commit it becomes.
      getCommit: async a => {
        const c = (sc.commits || {})[a.ref];
        if (!c) throw notFound();
        return { data: c };
      },
      compareCommits: async a => ({
        data: { status: (sc.onDefault || []).includes(a.base) ? 'ahead' : 'diverged' },
      }),
      getContent: async a => {
        calls.push({ op: 'read', target: a.owner + '/' + a.repo + ':' + a.path, sha: a.ref });
        const f = (sc.files || {})[a.ref + ':' + a.path];
        if (f === undefined) throw notFound();
        return { data: { content: Buffer.from(f).toString('base64') } };
      },
    },
    pulls: {
      get: async a => {
        const pr = sc.prs[key(a)];
        if (!pr) throw notFound();
        if (pending[key(a)] !== undefined && pending[key(a)]-- === 0) {
          pr.head.sha = sc.updates[key(a)];
          pr.mergeable_state = 'clean';
        }
        return { data: JSON.parse(JSON.stringify(pr)) };
      },
      merge: async a => {
        if ((sc.mergeRaces || []).includes(key(a))) {
          sc.prs[key(a)].merged = true;
          throw Object.assign(new Error('Pull Request is not mergeable'), { status: 405 });
        }
        calls.push({ op: 'merge', target: key(a), sha: a.sha });
        return { data: {} };
      },
      update: async a => { calls.push({ op: 'close', target: key(a) }); return { data: {} }; },
      updateBranch: async a => {
        calls.push({ op: 'update', target: key(a), sha: a.expected_head_sha });
        const pr = sc.prs[key(a)];
        if ((sc.updates || {})[key(a)]) pending[key(a)] = sc.updateDelay || 0;
        return { data: {} };
      },
    },
  },
};
const core = {
  info() {}, warning() {},
  error: m => calls.push({ op: 'error', target: m }),
  summary: { addHeading() { return this; }, addRaw() { return this; }, async write() {} },
  setFailed: m => calls.push({ op: 'failed', target: m }),
  setOutput: (k, v) => { outputs[k] = v; },
};
Object.assign(process.env, sc.env || {});
const AsyncFunction = (async () => {}).constructor;
new AsyncFunction('github', 'context', 'core', 'require', body)(github, sc.context, core, require)
  .then(() => console.log(JSON.stringify({ calls, outputs })))
  .catch(e => { console.error(e); process.exit(1); });
`

type markerComment struct {
	User struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"user"`
	Body string `json:"body"`
}

type markerScenario struct {
	Context     map[string]any            `json:"context"`
	Env         map[string]string         `json:"env"`
	Comments    []markerComment           `json:"comments"`
	Permissions map[string]string         `json:"permissions"`
	PRs         map[string]map[string]any `json:"prs"`
	// Updates maps a PR key to the head SHA updateBranch moves it to.
	Updates map[string]string         `json:"updates,omitempty"`
	Commits map[string]map[string]any `json:"commits,omitempty"`
	// Files maps "<sha>:<path>" to the file content at that commit.
	Files map[string]string `json:"files,omitempty"`
	// UpdateDelay is how many reads of the PR pass before a branch update lands.
	UpdateDelay int `json:"updateDelay,omitempty"`
	// OnDefault lists the commits on the default branch.
	OnDefault []string `json:"onDefault,omitempty"`
	// MergeRaces lists the PRs merged by someone else just before this run merges them.
	MergeRaces []string `json:"mergeRaces,omitempty"`
}

type markerCall struct {
	Op     string `json:"op"`
	Target string `json:"target"`
	SHA    string `json:"sha"`
}

type markerResult struct {
	Calls   []markerCall      `json:"calls"`
	Outputs map[string]string `json:"outputs"`
}

const (
	trustOrg  = "giantswarm"
	trustRepo = "antonia-management-clusters"
	trustInst = "anti1"
)

func comment(login, typ, body string) markerComment {
	c := markerComment{Body: body}
	c.User.Login = login
	c.User.Type = typ
	return c
}

func bot(body string) markerComment          { return comment("github-actions[bot]", "Bot", body) }
func mctl(body string) markerComment         { return comment("giantswarm-mctlbot[bot]", "Bot", body) }
func human(login, body string) markerComment { return comment(login, "User", body) }

// openPR is an open, clean PR from a branch of the same repository.
func openPR(repo string, number int, head string) (string, map[string]any) {
	full := trustOrg + "/" + repo
	return full + "#" + itoa(number), map[string]any{
		"number":          number,
		"state":           "open",
		"merged":          false,
		"mergeable_state": "clean",
		"node_id":         full + "#" + itoa(number),
		"head":            map[string]any{"ref": head, "sha": headSHA(repo, number), "repo": map[string]any{"full_name": full}},
		"base":            map[string]any{"ref": "main", "repo": map[string]any{"full_name": full, "default_branch": "main"}},
	}
}

func headSHA(repo string, number int) string { return "sha-" + repo + "-" + itoa(number) }

// commit is a commit with the given parents, as repos.getCommit returns it.
func commit(parents ...string) map[string]any {
	ps := []map[string]string{}
	for _, p := range parents {
		ps = append(ps, map[string]string{"sha": p})
	}
	return map[string]any{"parents": ps}
}

// landedSHAs checks that every merge and auto-merge names the head commit the script read.
func landedSHAs(t *testing.T, res markerResult, all map[string]map[string]any) {
	t.Helper()
	for _, c := range res.Calls {
		if c.Op != "merge" && c.Op != "automerge" {
			continue
		}
		want := all[c.Target]["head"].(map[string]any)["sha"]
		if c.SHA != want {
			t.Errorf("%s of %s pinned %q, want the checked head %q", c.Op, c.Target, c.SHA, want)
		}
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// prs indexes PRs by their owner/repo#number key.
func prs(keys []string, values ...map[string]any) map[string]map[string]any {
	m := map[string]map[string]any{}
	for i, k := range keys {
		m[k] = values[i]
	}
	return m
}

func url(repo string, number int) string {
	return "https://github.com/" + trustOrg + "/" + repo + "/pull/" + itoa(number)
}

func configMarkers(ccr, inst string) string {
	return "### ✅ `mctl config` completed\n\n- **CCR PR:** " + ccr + "\n- **Installations PR:** " + inst
}

func resultMarkers(inst, fleet string) string {
	return "<!-- mctl-create-mc-result-" + trustInst + " -->\n### ✅ `" + trustInst + "` created successfully\n\n" +
		"- **Installations PR:** " + inst + "\n- **Teleport fleet PR:** " + fleet
}

// mctlScript returns the github-script body of a step.
func mctlScript(t *testing.T, name, step string) string {
	t.Helper()

	wf := parseMctl(t, name, mctlRenders(t, false)[name])
	for _, job := range wf.Jobs {
		for _, s := range job.Steps {
			if s.Name == step {
				return s.With["script"]
			}
		}
	}
	t.Fatalf("step %q not found in %s", step, name)
	return ""
}

func runMarkerScript(t *testing.T, script string, sc markerScenario) markerResult {
	t.Helper()

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node on PATH (the GitHub-hosted runner has one; github-script runs on it)")
	}
	if sc.Context == nil {
		sc.Context = map[string]any{}
	}
	if _, ok := sc.Context["repo"]; !ok {
		sc.Context["repo"] = map[string]string{"owner": trustOrg, "repo": trustRepo}
	}

	dir := t.TempDir()
	scenario, err := json.Marshal(sc)
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"harness.js": markerHarness, "scenario.json": string(scenario), "script.js": script} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cmd := exec.CommandContext(t.Context(), node, filepath.Join(dir, "harness.js"), filepath.Join(dir, "scenario.json"), filepath.Join(dir, "script.js")) // #nosec G204 -- the script is the rendered template, test-only
	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("node: %v\n%s", err, stderr)
	}
	var res markerResult
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("harness output %q: %v", out, err)
	}
	return res
}

func ops(res markerResult, op string) []string {
	var got []string
	for _, c := range res.Calls {
		if c.Op == op {
			got = append(got, c.Target)
		}
	}
	return got
}

func Test_MctlCreateMcMarkerTrust(t *testing.T) {
	script := mctlScript(t, "create_mc", "Merge CCR PR")

	ccrKey, ccr := openPR("antonia-configs", 1, trustInst+"-config")
	otherKey, other := openPR("antonia-configs", 2, "attacker")
	foreignKey, foreign := openPR("devctl", 3, trustInst+"-config")
	forkKey, fork := openPR("antonia-configs", 4, trustInst+"-config")
	fork["head"].(map[string]any)["repo"] = map[string]any{"full_name": "someone/antonia-configs"}
	closedKey, closed := openPR("antonia-configs", 5, trustInst+"-config")
	closed["state"] = "closed"
	sideKey, side := openPR("antonia-configs", 7, trustInst+"-config")
	side["base"].(map[string]any)["ref"] = "release"
	all := prs([]string{ccrKey, otherKey, foreignKey, forkKey, closedKey, sideKey}, ccr, other, foreign, fork, closed, side)
	inst := url("installations", 9)

	tests := []struct {
		name      string
		comments  []markerComment
		wantMerge string
	}{
		{
			name:      "config bot marker",
			comments:  []markerComment{bot(configMarkers(url("antonia-configs", 1), inst))},
			wantMerge: ccrKey,
		},
		{
			name: "newer marker from a read-only member is ignored",
			comments: []markerComment{
				bot(configMarkers(url("antonia-configs", 1), inst)),
				human("reader", "/create-mc\n"+configMarkers(url("devctl", 3), inst)),
			},
			wantMerge: ccrKey,
		},
		{
			name:     "marker from a writer must still name the configs repo",
			comments: []markerComment{human("writer", configMarkers(url("devctl", 3), inst))},
		},
		{
			name:     "CCR PR from another branch",
			comments: []markerComment{bot(configMarkers(url("antonia-configs", 2), inst))},
		},
		{
			name:     "CCR PR from a fork",
			comments: []markerComment{bot(configMarkers(url("antonia-configs", 4), inst))},
		},
		{
			name:     "installations marker outside installations",
			comments: []markerComment{bot(configMarkers(url("antonia-configs", 1), url("devctl", 3)))},
		},
		{
			name:     "CCR PR into another branch",
			comments: []markerComment{bot(configMarkers(url("antonia-configs", 7), inst))},
		},
		{
			name:     "CCR PR closed without merging",
			comments: []markerComment{bot(configMarkers(url("antonia-configs", 5), inst))},
		},
		{
			name:     "CCR PR that cannot be read",
			comments: []markerComment{bot(configMarkers(url("antonia-configs", 6), inst))},
		},
		{
			name:     "bot login typed by a user",
			comments: []markerComment{comment("github-actions[bot]", "User", configMarkers(url("antonia-configs", 1), inst))},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := runMarkerScript(t, script, markerScenario{
				Context:     map[string]any{"issue": map[string]int{"number": 10}},
				Env:         map[string]string{"INSTALLATION": trustInst},
				Comments:    tc.comments,
				Permissions: map[string]string{"reader": "read", "writer": "write"},
				PRs:         all,
			})
			merged := strings.Join(ops(res, "merge"), ",")
			if merged != tc.wantMerge {
				t.Errorf("merged %q, want %q (calls %+v)", merged, tc.wantMerge, res.Calls)
			}
			landedSHAs(t, res, all)
			if tc.wantMerge == "" && len(ops(res, "failed")) == 0 {
				t.Errorf("a refused marker must fail the job (calls %+v)", res.Calls)
			}
		})
	}
}

func Test_MctlMergeCascadeMarkerTrust(t *testing.T) {
	script := mctlScript(t, "merge_cascade", "Merge installations and teleport-fleet PRs")

	instKey, inst := openPR("installations", 1, trustInst+"-init")
	fleetKey, fleet := openPR("teleport-fleet", 2, "bot-"+trustInst+"-token")
	foreignKey, foreign := openPR("devctl", 3, trustInst+"-init")
	wrongKey, wrong := openPR("teleport-fleet", 4, "attacker")
	sideKey, side := openPR("installations", 5, trustInst+"-init")
	side["base"].(map[string]any)["ref"] = "release"
	all := prs([]string{instKey, fleetKey, foreignKey, wrongKey, sideKey}, inst, fleet, foreign, wrong, side)

	tests := []struct {
		name      string
		comments  []markerComment
		wantMerge string
		wantArmed string
	}{
		{
			name:      "result posted by Tekton's fallback",
			comments:  []markerComment{comment("tinkerers-ci[bot]", "Bot", resultMarkers(url("installations", 1), url("teleport-fleet", 2)))},
			wantMerge: instKey + "," + fleetKey,
		},
		{
			name:     "result from an unknown bot",
			comments: []markerComment{comment("renovate[bot]", "Bot", resultMarkers(url("installations", 1), url("teleport-fleet", 2)))},
		},
		{
			name:      "installations PR into another branch",
			comments:  []markerComment{bot(resultMarkers(url("installations", 5), url("teleport-fleet", 2)))},
			wantMerge: fleetKey,
		},
		{
			name:      "result from the mctl App",
			comments:  []markerComment{mctl(resultMarkers(url("installations", 1), url("teleport-fleet", 2)))},
			wantMerge: instKey + "," + fleetKey,
		},
		{
			name:      "result of a local run by a writer",
			comments:  []markerComment{human("writer", resultMarkers(url("installations", 1), url("teleport-fleet", 2)))},
			wantMerge: instKey + "," + fleetKey,
		},
		{
			name:     "forged result from a read-only member",
			comments: []markerComment{human("reader", resultMarkers(url("installations", 1), url("teleport-fleet", 2)))},
		},
		{
			name: "newer marker from a read-only member is ignored",
			comments: []markerComment{
				bot(resultMarkers(url("installations", 1), url("teleport-fleet", 2))),
				human("reader", "- **Installations PR:** "+url("devctl", 3)),
			},
			wantMerge: instKey + "," + fleetKey,
		},
		{
			name:      "trusted markers that name other repositories or branches",
			comments:  []markerComment{bot(resultMarkers(url("devctl", 3), url("teleport-fleet", 4)))},
			wantMerge: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := runMarkerScript(t, script, markerScenario{
				Context:     map[string]any{"payload": map[string]any{"pull_request": map[string]int{"number": 10}}},
				Env:         map[string]string{"BRANCH": trustInst + "-init-capa"},
				Comments:    tc.comments,
				Permissions: map[string]string{"reader": "read", "writer": "admin"},
				PRs:         all,
			})
			if got := strings.Join(ops(res, "merge"), ","); got != tc.wantMerge {
				t.Errorf("merged %q, want %q (calls %+v)", got, tc.wantMerge, res.Calls)
			}
			if got := strings.Join(ops(res, "automerge"), ","); got != tc.wantArmed {
				t.Errorf("armed auto-merge on %q, want %q (calls %+v)", got, tc.wantArmed, res.Calls)
			}
			landedSHAs(t, res, all)
		})
	}
}

// Test_MctlMergeCascadeLandsTheCheckedCommit pins the merge to the commit that was read
// and checked: a push to the branch after that must not land unreviewed.
func Test_MctlMergeCascadeLandsTheCheckedCommit(t *testing.T) {
	script := mctlScript(t, "merge_cascade", "Merge installations and teleport-fleet PRs")
	const (
		repo     = "example-management-clusters"
		contacts = "slack:\n  support:\n    - '#support-example'\naccountEngineer: someone\naccountEngineersHandle: '@someone'\n"
		path     = trustInst + "/cluster.yaml"
	)

	tests := []struct {
		name string
		// instState and fleetState are the mergeable_state of the two PRs.
		instState, fleetState string
		// updated is the commit updateBranch moves the installations PR to.
		updated string
		// delay is how many reads of the PR pass before GitHub applies the update.
		delay    int
		commits  map[string]map[string]any
		files    map[string]string
		wantInst string
		wantRead string
	}{
		{
			name:       "clean PRs",
			instState:  "clean",
			fleetState: "blocked",
			files:      map[string]string{headSHA("installations", 1) + ":" + path: contacts},
			wantInst:   headSHA("installations", 1),
			wantRead:   headSHA("installations", 1),
		},
		{
			name:       "behind branch updated by GitHub",
			instState:  "behind",
			fleetState: "blocked",
			updated:    "sha-update",
			commits:    map[string]map[string]any{"sha-update": commit(headSHA("installations", 1), "sha-main")},
			files:      map[string]string{"sha-update:" + path: contacts},
			wantInst:   "sha-update",
			wantRead:   "sha-update",
		},
		{
			name:       "branch update GitHub applies a little later",
			instState:  "behind",
			fleetState: "blocked",
			updated:    "sha-update",
			delay:      3,
			commits:    map[string]map[string]any{"sha-update": commit(headSHA("installations", 1), "sha-main")},
			files:      map[string]string{"sha-update:" + path: contacts},
			wantInst:   "sha-update",
			wantRead:   "sha-update",
		},
		{
			name:       "branch update that never lands merges the checked commit",
			instState:  "behind",
			fleetState: "blocked",
			files:      map[string]string{headSHA("installations", 1) + ":" + path: contacts},
			wantInst:   headSHA("installations", 1),
			wantRead:   headSHA("installations", 1),
		},
		{
			name:       "merge commit pushed with a parent not on the default branch",
			instState:  "behind",
			fleetState: "blocked",
			updated:    "sha-evil-merge",
			commits:    map[string]map[string]any{"sha-evil-merge": commit(headSHA("installations", 1), "sha-evil")},
			files:      map[string]string{"sha-evil-merge:" + path: contacts},
		},
		{
			name:       "behind branch pushed to after it was read",
			instState:  "behind",
			fleetState: "blocked",
			updated:    "sha-pushed",
			commits:    map[string]map[string]any{"sha-pushed": commit("sha-other")},
			files:      map[string]string{"sha-pushed:" + path: contacts},
		},
		{
			name:       "contacts missing at the commit that would be merged",
			instState:  "behind",
			fleetState: "blocked",
			updated:    "sha-update",
			commits:    map[string]map[string]any{"sha-update": commit(headSHA("installations", 1), "sha-main")},
			files: map[string]string{
				headSHA("installations", 1) + ":" + path: contacts,
				"sha-update:" + path:                     "accountEngineer: FILL_IN_NAME\n",
			},
			wantRead: "sha-update",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			instKey, inst := openPR("installations", 1, trustInst+"-init")
			inst["mergeable_state"] = tc.instState
			fleetKey, fleet := openPR("teleport-fleet", 2, "bot-"+trustInst+"-token")
			fleet["mergeable_state"] = tc.fleetState
			sc := markerScenario{
				Context: map[string]any{
					"repo":    map[string]string{"owner": trustOrg, "repo": repo},
					"payload": map[string]any{"pull_request": map[string]int{"number": 10}},
				},
				Env:         map[string]string{"BRANCH": trustInst + "-init"},
				Comments:    []markerComment{bot(resultMarkers(url("installations", 1), url("teleport-fleet", 2)))},
				PRs:         prs([]string{instKey, fleetKey}, inst, fleet),
				Commits:     tc.commits,
				Files:       tc.files,
				UpdateDelay: tc.delay,
				OnDefault:   []string{"sha-main"},
			}
			if tc.updated != "" {
				sc.Updates = map[string]string{instKey: tc.updated}
			}
			res := runMarkerScript(t, script, sc)

			var merged, updated, read []string
			for _, c := range res.Calls {
				switch c.Op {
				case "merge":
					merged = append(merged, c.Target+"@"+c.SHA)
				case "update":
					updated = append(updated, c.Target+"@"+c.SHA)
				case "read":
					read = append(read, c.SHA)
				}
			}
			want := ""
			if tc.wantInst != "" {
				want = instKey + "@" + tc.wantInst
			}
			if got := strings.Join(merged, ","); got != want {
				t.Errorf("merged %q, want %q (calls %+v)", got, want, res.Calls)
			}
			if tc.instState == "behind" {
				if got, want := strings.Join(updated, ","), instKey+"@"+headSHA("installations", 1); got != want {
					t.Errorf("updated %q, want %q expecting the checked head", got, want)
				}
			}
			if tc.wantRead != "" && (len(read) != 1 || read[0] != tc.wantRead) {
				t.Errorf("read cluster.yaml at %v, want %s", read, tc.wantRead)
			}
			if tc.wantInst == "" && len(ops(res, "failed")) == 0 {
				t.Errorf("a PR left unmerged must fail the job (calls %+v)", res.Calls)
			}
			armed := ""
			for _, c := range res.Calls {
				if c.Op == "automerge" {
					armed = c.Target + "@" + c.SHA
				}
			}
			if want := fleetKey + "@" + headSHA("teleport-fleet", 2); armed != want {
				t.Errorf("armed %q, want %q", armed, want)
			}
		})
	}
}

func Test_MctlCloseCascadeMarkerTrust(t *testing.T) {
	find := mctlScript(t, "close_cascade", "Find config PR URLs from comments")
	closeScript := mctlScript(t, "close_cascade", "Close related PRs")

	ccrKey, ccr := openPR("antonia-configs", 1, trustInst+"-config")
	instKey, inst := openPR("installations", 2, trustInst+"-init")
	fleetKey, fleet := openPR("teleport-fleet", 3, "bot-"+trustInst+"-token")
	foreignKey, foreign := openPR("devctl", 4, trustInst+"-config")
	wrongKey, wrong := openPR("installations", 5, "attacker")
	sideKey, side := openPR("installations", 6, trustInst+"-init")
	side["base"].(map[string]any)["ref"] = "release"
	mergedKey, merged := openPR("teleport-fleet", 7, "bot-"+trustInst+"-token")
	merged["state"], merged["merged"] = "closed", true
	all := prs([]string{ccrKey, instKey, fleetKey, foreignKey, wrongKey, sideKey, mergedKey}, ccr, inst, fleet, foreign, wrong, side, merged)

	tests := []struct {
		name      string
		comments  []markerComment
		wantClose string
	}{
		{
			name: "markers from the bots",
			comments: []markerComment{
				bot(configMarkers(url("antonia-configs", 1), url("installations", 2))),
				bot(resultMarkers(url("installations", 2), url("teleport-fleet", 3))),
			},
			wantClose: ccrKey + "," + instKey + "," + fleetKey,
		},
		{
			name: "newer markers from a read-only member are ignored",
			comments: []markerComment{
				bot(configMarkers(url("antonia-configs", 1), url("installations", 2))),
				human("reader", configMarkers(url("devctl", 4), url("devctl", 4))),
			},
			wantClose: ccrKey + "," + instKey,
		},
		{
			name:     "trusted markers that name other repositories or branches",
			comments: []markerComment{human("writer", configMarkers(url("devctl", 4), url("installations", 5)))},
		},
		{
			name: "PR into another branch and an already merged PR",
			comments: []markerComment{
				bot(configMarkers(url("antonia-configs", 1), url("installations", 6))),
				bot(resultMarkers(url("installations", 6), url("teleport-fleet", 7))),
			},
			wantClose: ccrKey,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sc := markerScenario{
				Context:     map[string]any{"payload": map[string]any{"pull_request": map[string]int{"number": 10}}},
				Comments:    tc.comments,
				Permissions: map[string]string{"reader": "triage", "writer": "write"},
				PRs:         all,
			}
			found := runMarkerScript(t, find, sc)
			sc.Env = map[string]string{
				"BRANCH":                trustInst + "-init",
				"CCR_PR_URL":            found.Outputs["ccr_pr_url"],
				"INSTALLATIONS_PR_URL":  found.Outputs["installations_pr_url"],
				"TELEPORT_FLEET_PR_URL": found.Outputs["teleport_fleet_pr_url"],
			}
			res := runMarkerScript(t, closeScript, sc)
			if got := strings.Join(ops(res, "close"), ","); got != tc.wantClose {
				t.Errorf("closed %q, want %q (calls %+v)", got, tc.wantClose, res.Calls)
			}
		})
	}
}

// Test_MctlCreateMcOnlyOnInitBranches runs the step that derives the installation:
// /create-mc on any other PR stops before the App token is minted.
func Test_MctlCreateMcOnlyOnInitBranches(t *testing.T) {
	wf := parseMctl(t, "create_mc", mctlRenders(t, false)["create_mc"])
	check, author, mint := -1, -1, -1
	for _, job := range wf.Jobs {
		for i, s := range job.Steps {
			if s.Name == "Derive installation name" {
				check = i
			}
			if s.Name == "Check commenter" {
				author = i
			}
			if strings.HasPrefix(s.Uses, "actions/create-github-app-token@") && mint < 0 {
				mint = i
			}
		}
	}
	if check < 0 || author < 0 || mint < 0 || check > mint || author > mint {
		t.Fatalf("create_mc must check the commenter (step %d) and the branch (step %d) before it mints the App token (step %d)", author, check, mint)
	}
	script := mctlStepScript(t, "create_mc", "Derive installation name")

	for branch, want := range map[string]string{
		"jackal-init":         "jackal",
		"jackal-init-capa":    "jackal",
		"xatest3-init-capvcd": "xatest3",
		"main-update":         "",
		"fix-init-script":     "",
		"jackal-init-foo":     "",
		"-init":               "",
	} {
		t.Run(branch, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "output")
			cmd := exec.CommandContext(t.Context(), "bash", "-eo", "pipefail", "-c", script) // #nosec G204 -- the script is the rendered template, test-only
			cmd.Env = append(os.Environ(), "BRANCH="+branch, "GITHUB_OUTPUT="+out)
			err := cmd.Run()
			if want == "" {
				if err == nil {
					t.Errorf("branch %q passed, want the step to fail", branch)
				}
				return
			}
			if err != nil {
				t.Fatalf("branch %q failed: %v", branch, err)
			}
			raw, err := os.ReadFile(out) // #nosec G304 -- a temp file of this test
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(string(raw)); got != "name="+want {
				t.Errorf("branch %q gave %q, want name=%s", branch, got, want)
			}
		})
	}
}

// Test_MctlCreateMcRefusesForks runs "Get PR branch": an init-named branch in a fork is not an init PR.
func Test_MctlCreateMcRefusesForks(t *testing.T) {
	script := mctlScript(t, "create_mc", "Get PR branch")

	for name, headRepo := range map[string]string{
		"same repository": trustOrg + "/" + trustRepo,
		"fork":            "someone/" + trustRepo,
	} {
		t.Run(name, func(t *testing.T) {
			key, pr := openPR(trustRepo, 10, trustInst+"-init")
			pr["head"].(map[string]any)["repo"] = map[string]any{"full_name": headRepo}
			pr["head"].(map[string]any)["label"] = "someone:" + trustInst + "-init"
			res := runMarkerScript(t, script, markerScenario{
				Context: map[string]any{"issue": map[string]int{"number": 10}},
				PRs:     prs([]string{key}, pr),
			})
			fork := name == "fork"
			if failed := len(ops(res, "failed")) > 0; failed != fork {
				t.Errorf("failed = %v, want %v (calls %+v)", failed, fork, res.Calls)
			}
			if got := res.Outputs["branch"]; (got == "") != fork {
				t.Errorf("branch output = %q for %s", got, name)
			}
		})
	}
}

// Test_MctlCreateMcChecksCommenter runs "Check commenter": author_association lets
// read-only org members through, so the permission on this repository decides.
func Test_MctlCreateMcChecksCommenter(t *testing.T) {
	script := mctlScript(t, "create_mc", "Check commenter")

	for name, tc := range map[string]struct {
		login, typ string
		allowed    bool
	}{
		"writer":             {"writer", "User", true},
		"admin":              {"admin", "User", true},
		"read-only member":   {"reader", "User", false},
		"not a collaborator": {"stranger", "User", false},
		"bot":                {"giantswarm-mctlbot[bot]", "Bot", false},
		"unreadable":         {"flaky", "User", false},
	} {
		t.Run(name, func(t *testing.T) {
			res := runMarkerScript(t, script, markerScenario{
				Context: map[string]any{"payload": map[string]any{"comment": map[string]any{
					"user": map[string]string{"login": tc.login, "type": tc.typ},
				}}},
				Permissions: map[string]string{"writer": "write", "admin": "admin", "reader": "read", "giantswarm-mctlbot[bot]": "write", "flaky": "error"},
			})
			if name == "unreadable" && !strings.Contains(res.Outputs["reason"], "Could not read the permission of flaky (502)") {
				t.Errorf("reason = %q, want the read error", res.Outputs["reason"])
			}
			if failed := len(ops(res, "failed")) > 0; failed == tc.allowed {
				t.Errorf("failed = %v for %s (calls %+v)", failed, name, res.Calls)
			}
			if got := res.Outputs["reason"]; (got == "") != tc.allowed {
				t.Errorf("reason = %q for %s", got, name)
			}
		})
	}
}

// Test_MctlCreateMcUnreadablePR: a failed pulls.get is reported as such, not as "not an init PR".
func Test_MctlCreateMcUnreadablePR(t *testing.T) {
	res := runMarkerScript(t, mctlScript(t, "create_mc", "Get PR branch"), markerScenario{
		Context: map[string]any{"issue": map[string]int{"number": 10}},
		PRs:     map[string]map[string]any{},
	})
	if reason := res.Outputs["reason"]; !strings.Contains(reason, "Could not read this PR (404)") {
		t.Errorf("reason = %q, want the read error", reason)
	}
}

// Test_MctlCreateMcClosedInitPR: /create-mc on a merged or closed init PR does not start the pipeline again.
func Test_MctlCreateMcClosedInitPR(t *testing.T) {
	script := mctlScript(t, "create_mc", "Get PR branch")
	key, pr := openPR(trustRepo, 10, trustInst+"-init")
	pr["state"], pr["merged"] = "closed", true
	res := runMarkerScript(t, script, markerScenario{
		Context: map[string]any{"issue": map[string]int{"number": 10}},
		PRs:     prs([]string{key}, pr),
	})
	if reason := res.Outputs["reason"]; !strings.Contains(reason, "this one is merged") || res.Outputs["branch"] != "" {
		t.Errorf("reason = %q, branch = %q", reason, res.Outputs["branch"])
	}
}

// Test_MctlCreateMcCCRMergedMeanwhile: a CCR PR merged by another run just before this one is not an error.
func Test_MctlCreateMcCCRMergedMeanwhile(t *testing.T) {
	ccrKey, ccr := openPR("antonia-configs", 1, trustInst+"-config")
	res := runMarkerScript(t, mctlScript(t, "create_mc", "Merge CCR PR"), markerScenario{
		Context:    map[string]any{"issue": map[string]int{"number": 10}},
		Env:        map[string]string{"INSTALLATION": trustInst},
		Comments:   []markerComment{bot(configMarkers(url("antonia-configs", 1), url("installations", 9)))},
		PRs:        prs([]string{ccrKey}, ccr),
		MergeRaces: []string{ccrKey},
	})
	if failed := ops(res, "failed"); len(failed) > 0 {
		t.Errorf("failed %v, want the run to go on", failed)
	}
}

// Test_MctlCloseCascadeUnreadablePR: a PR that cannot be read is reported as such, not as a failed close.
func Test_MctlCloseCascadeUnreadablePR(t *testing.T) {
	res := runMarkerScript(t, mctlScript(t, "close_cascade", "Close related PRs"), markerScenario{
		Context: map[string]any{"payload": map[string]any{"pull_request": map[string]int{"number": 10}}},
		Env:     map[string]string{"BRANCH": trustInst + "-init", "INSTALLATIONS_PR_URL": url("installations", 8)},
		PRs:     map[string]map[string]any{},
	})
	body := strings.Join(ops(res, "comment"), "")
	if !strings.Contains(body, "**Could not be read, left as they are:**") || strings.Contains(body, "Failed to close") {
		t.Errorf("comment %q", body)
	}
}
