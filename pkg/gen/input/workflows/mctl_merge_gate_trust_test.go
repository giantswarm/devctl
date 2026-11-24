package workflows

import (
	"strings"
	"testing"
)

// The merge gate publishes the required mctl/ready-to-merge status from init PR
// comments (giantswarm/giantswarm#39467). It must trust the same authors and PRs
// as the merge cascade it guards.
func Test_MctlMergeGateMarkerTrust(t *testing.T) {
	script := mctlScript(t, "merge_gate", "Evaluate and publish status")

	initKey, initPR := openPR(trustRepo, 10, trustInst+"-init-capa")
	instKey, inst := openPR("installations", 1, trustInst+"-init")
	fleetKey, fleet := openPR("teleport-fleet", 2, "bot-"+trustInst+"-token")
	foreignKey, foreign := openPR("devctl", 3, trustInst+"-init")
	wrongKey, wrong := openPR("teleport-fleet", 4, "attacker")
	forkKey, fork := openPR("installations", 5, trustInst+"-init")
	fork["head"].(map[string]any)["repo"] = map[string]any{"full_name": "someone/installations"}
	sideKey, side := openPR("teleport-fleet", 6, "bot-"+trustInst+"-token")
	side["base"].(map[string]any)["ref"] = "release"
	all := prs([]string{initKey, instKey, fleetKey, foreignKey, wrongKey, forkKey, sideKey}, initPR, inst, fleet, foreign, wrong, fork, side)

	good := resultMarkers(url("installations", 1), url("teleport-fleet", 2))
	failed := "<!-- mctl-create-mc-result-" + trustInst + " -->\n### ❌ `" + trustInst + "` bootstrap failed"

	tests := []struct {
		name     string
		comments []markerComment
		want     string
		wantText string
		// wantError is the error the workflow logs, if any.
		wantError string
	}{
		{
			name:     "result from the config and Tekton bot",
			comments: []markerComment{bot(good)},
			want:     "success",
		},
		{
			name:     "result from the mctl App",
			comments: []markerComment{mctl(good)},
			want:     "success",
		},
		{
			name:     "result posted by Tekton's fallback",
			comments: []markerComment{comment("tinkerers-ci[bot]", "Bot", good)},
			want:     "success",
		},
		{
			name:     "result of a local run by a writer",
			comments: []markerComment{human("writer", good)},
			want:     "success",
		},
		{
			name:     "forged result from a read-only member",
			comments: []markerComment{human("reader", good)},
			want:     "failure",
			wantText: "No /create-mc run",
		},
		{
			name:     "result from a user who is not a collaborator",
			comments: []markerComment{human("ghost", good)},
			want:     "failure",
			wantText: "No /create-mc run",
		},
		{
			name:      "result from a user whose permission cannot be read",
			comments:  []markerComment{bot(good), human("flaky", good)},
			want:      "error",
			wantText:  "Rerun this check: could not read the permission of flaky",
			wantError: "Could not read the permission of flaky",
		},
		{
			name:      "quoted result from a user whose permission cannot be read",
			comments:  []markerComment{bot(good), human("flaky", "> - **Installations PR:** "+url("installations", 1))},
			want:      "error",
			wantError: "Could not read the permission of flaky",
		},
		{
			name:      "plain comment from a user whose permission cannot be read",
			comments:  []markerComment{bot(good), human("flaky", "/create-mc please")},
			want:      "success",
			wantError: "Could not read the permission of flaky",
		},
		{
			name:     "forged result from an unknown bot",
			comments: []markerComment{comment("renovate[bot]", "Bot", good)},
			want:     "failure",
			wantText: "No /create-mc run",
		},
		{
			name:     "bot login typed by a user",
			comments: []markerComment{comment("github-actions[bot]", "User", good)},
			want:     "failure",
			wantText: "No /create-mc run",
		},
		{
			name:     "forged success after a failed run",
			comments: []markerComment{bot(failed), human("reader", good)},
			want:     "failure",
			wantText: "did not succeed",
		},
		{
			name:     "newer marker from a read-only member is ignored",
			comments: []markerComment{bot(good), human("reader", "- **Teleport fleet PR:** "+url("teleport-fleet", 4))},
			want:     "success",
		},
		{
			name:     "trusted marker outside the target repository",
			comments: []markerComment{bot(resultMarkers(url("devctl", 3), url("teleport-fleet", 2)))},
			want:     "failure",
			wantText: "not in giantswarm/installations",
		},
		{
			name:     "trusted marker from another branch",
			comments: []markerComment{bot(resultMarkers(url("installations", 1), url("teleport-fleet", 4)))},
			want:     "failure",
			wantText: "not from bot-" + trustInst + "-token into",
		},
		{
			name:     "trusted marker into another base branch",
			comments: []markerComment{bot(resultMarkers(url("installations", 1), url("teleport-fleet", 6)))},
			want:     "failure",
			wantText: "into the default branch",
		},
		{
			name:     "trusted marker from a fork",
			comments: []markerComment{bot(resultMarkers(url("installations", 5), url("teleport-fleet", 2)))},
			want:     "failure",
			wantText: "not from " + trustInst + "-init into",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := runMarkerScript(t, script, markerScenario{
				Context: map[string]any{
					"eventName": "issue_comment",
					"issue":     map[string]int{"number": 10},
					"serverUrl": "https://github.com",
					"runId":     1,
				},
				Comments:    tc.comments,
				Permissions: map[string]string{"reader": "read", "writer": "write", "flaky": "unavailable"},
				PRs:         all,
			})
			statuses := ops(res, "status")
			if len(statuses) != 1 {
				t.Fatalf("published %d statuses, want 1 (calls %+v)", len(statuses), res.Calls)
			}
			if !strings.HasPrefix(statuses[0], tc.want+":") || !strings.Contains(statuses[0], tc.wantText) {
				t.Errorf("published %q, want %s with %q", statuses[0], tc.want, tc.wantText)
			}
			errors := ops(res, "error")
			if tc.wantError == "" && len(errors) > 0 || tc.wantError != "" && (len(errors) != 1 || !strings.Contains(errors[0], tc.wantError)) {
				t.Errorf("logged errors %q, want %q", errors, tc.wantError)
			}
		})
	}
}

// A customer that is not a test customer also has its installations PR's
// contacts checked, which must read cluster.yaml only from mctl's PR.
func Test_MctlMergeGateCustomerContacts(t *testing.T) {
	script := mctlScript(t, "merge_gate", "Evaluate and publish status")

	const customerRepo = "acme-management-clusters"
	initKey, initPR := openPR(customerRepo, 10, trustInst+"-init")
	instKey, inst := openPR("installations", 1, trustInst+"-init")
	fleetKey, fleet := openPR("teleport-fleet", 2, "bot-"+trustInst+"-token")
	foreignKey, foreign := openPR("devctl", 3, trustInst+"-init")
	wrongKey, wrong := openPR("installations", 4, "attacker")
	forkKey, fork := openPR("installations", 5, trustInst+"-init")
	fork["head"].(map[string]any)["repo"] = map[string]any{"full_name": "someone/installations"}
	all := prs([]string{initKey, instKey, fleetKey, foreignKey, wrongKey, forkKey}, initPR, inst, fleet, foreign, wrong, fork)

	clusterYAML := headSHA("installations", 1) + ":" + trustInst + "/cluster.yaml"
	filled := "slack:\n  support:\n    - '#support-acme'\naccountEngineer: jane\naccountEngineersHandle: '@jane'\n"
	unfilled := "slack:\n  support:\n    - '#support-acme'\naccountEngineer: FILL_IN_AE\naccountEngineersHandle: '@jane'\n"

	tests := []struct {
		name     string
		inst     string
		extra    []markerComment
		file     string
		want     string
		wantText string
	}{
		{name: "contacts filled in", inst: url("installations", 1), file: filled, want: "success"},
		{name: "contacts missing", inst: url("installations", 1), file: unfilled, want: "failure", wantText: "lacks accountEngineer"},
		{name: "installations marker outside installations", inst: url("devctl", 3), file: filled, want: "failure", wantText: "not in giantswarm/installations"},
		{name: "installations PR from another branch", inst: url("installations", 4), file: filled, want: "failure", wantText: "not from " + trustInst + "-init into"},
		{name: "installations PR from a fork", inst: url("installations", 5), file: filled, want: "failure", wantText: "not from " + trustInst + "-init into"},
		{
			name:     "newer installations marker from a read-only member is ignored",
			inst:     url("installations", 1),
			extra:    []markerComment{human("reader", "- **Installations PR:** "+url("installations", 6))},
			file:     unfilled,
			want:     "failure",
			wantText: "lacks accountEngineer",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := runMarkerScript(t, script, markerScenario{
				Context: map[string]any{
					"eventName": "issue_comment",
					"issue":     map[string]int{"number": 10},
					"serverUrl": "https://github.com",
					"runId":     1,
					"repo":      map[string]string{"owner": trustOrg, "repo": customerRepo},
				},
				Comments:    append([]markerComment{bot(resultMarkers(tc.inst, url("teleport-fleet", 2)))}, tc.extra...),
				Permissions: map[string]string{"reader": "read"},
				PRs:         all,
				Files:       map[string]string{clusterYAML: tc.file},
			})
			statuses := ops(res, "status")
			if len(statuses) != 1 {
				t.Fatalf("published %d statuses, want 1 (calls %+v)", len(statuses), res.Calls)
			}
			if !strings.HasPrefix(statuses[0], tc.want+":") || !strings.Contains(statuses[0], tc.wantText) {
				t.Errorf("published %q, want %s with %q", statuses[0], tc.want, tc.wantText)
			}
		})
	}
}
