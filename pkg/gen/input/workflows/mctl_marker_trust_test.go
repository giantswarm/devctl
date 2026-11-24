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
const github = {
  paginate: async (fn, args) => (await fn(args)).data,
  graphql: async (q, v) => { calls.push({ op: 'automerge', target: v.id }); return {}; },
  rest: {
    issues: {
      listComments: async () => ({ data: sc.comments }),
      createComment: async a => { calls.push({ op: 'comment', target: a.body }); return { data: {} }; },
      updateComment: async () => ({ data: {} }),
    },
    reactions: { createForIssueComment: async () => ({ data: {} }) },
    repos: {
      getCollaboratorPermissionLevel: async a => {
        const p = (sc.permissions || {})[a.username];
        if (!p) throw notFound();
        return { data: { permission: p } };
      },
    },
    pulls: {
      get: async a => {
        const pr = sc.prs[key(a)];
        if (!pr) throw notFound();
        return { data: pr };
      },
      merge: async a => { calls.push({ op: 'merge', target: key(a) }); return { data: {} }; },
      update: async a => { calls.push({ op: 'close', target: key(a) }); return { data: {} }; },
      updateBranch: async () => ({ data: {} }),
    },
  },
};
const core = {
  info() {}, warning() {},
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
}

type markerCall struct {
	Op     string `json:"op"`
	Target string `json:"target"`
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
		"head":            map[string]any{"ref": head, "sha": "abc", "repo": map[string]any{"full_name": full}},
		"base":            map[string]any{"repo": map[string]any{"full_name": full}},
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
	sc.Context["repo"] = map[string]string{"owner": trustOrg, "repo": trustRepo}

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
	all := prs([]string{ccrKey, otherKey, foreignKey, forkKey}, ccr, other, foreign, fork)
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
	all := prs([]string{instKey, fleetKey, foreignKey, wrongKey}, inst, fleet, foreign, wrong)

	tests := []struct {
		name      string
		comments  []markerComment
		wantMerge string
		wantArmed string
	}{
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
	all := prs([]string{ccrKey, instKey, fleetKey, foreignKey, wrongKey}, ccr, inst, fleet, foreign, wrong)

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
