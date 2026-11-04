package promote

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/releasepromote"
)

// Exercise the command's exit/document contract through the real GitHub file
// reader. The branch and the latest candidate deliberately have different
// notes from the selected candidate, so checking either cannot pass this test.
func TestRunUpgradeNotesPreflight(t *testing.T) {
	cases := []struct {
		name       string
		notes      string
		status     int
		version    string
		branch     string
		dryRun     bool
		refused    bool
		unreadable bool
	}{
		{name: "unreleased notes", notes: "# Upgrade\n\n## Next\n\n- Migrate settings.\n", refused: true},
		{name: "empty Next section", notes: "## Next\n\n## v1.2.0\nReleased notes.\n", refused: true},
		{name: "heading without final newline", notes: "## Next", refused: true},
		{name: "annotated heading", notes: "## Next - breaking changes\n", refused: true},
		{name: "later Next section with CRLF", notes: "## v1.2.0\r\nReleased.\r\n## Next\r\nChange.\r\n", refused: true},
		{name: "dry run also refuses", notes: "## Next\nChange.\n", dryRun: true, refused: true},
		{name: "named older candidate still has Next", notes: "## Next\nChange.\n", version: "v1.2.3-rc.14", refused: true},
		{name: "maintenance branch still has Next", notes: "## Next\nChange.\n", branch: "release-1.3", refused: true},
		{name: "missing file", status: http.StatusNotFound},
		{name: "empty file"},
		{name: "versioned notes", notes: "# Upgrade\n\n## v1.3.0\n- Migrate settings.\n"},
		{name: "inline and other level mentions", notes: "Rename `## Next` before release.\n# Next\n### Next\n## v1.3.0\nNotes.\n"},
		{name: "dry run with versioned notes", notes: "## v1.3.0\nNotes.\n", dryRun: true},
		{name: "named older candidate has versioned notes", notes: "## v1.2.3\nNotes.\n", version: "v1.2.3-rc.14"},
		{name: "read forbidden", status: http.StatusForbidden, unreadable: true},
		{name: "read unavailable", status: http.StatusServiceUnavailable, unreadable: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const repository = "giantswarm/kserve"
			candidate := "v1.3.0-rc.1"
			routes := promoteRoutes(repository)
			if tc.version != "" {
				candidate = tc.version
				routes = streamRoutes(repository)
			}
			if tc.branch != "" {
				// The candidate is reachable from the requested maintenance branch.
				routes["GET /repos/"+repository+"/compare/v1.2.0..."+tc.branch] = []sequence.Response{{Body: map[string]any{"status": "ahead"}}}
				routes["GET /repos/"+repository+"/compare/"+candidate+"..."+tc.branch] = []sequence.Response{{Body: map[string]any{"status": "identical"}}}
			}
			const upgradePath = "/repos/" + repository + "/contents/UPGRADE.md"
			file := func(notes string) sequence.Response {
				return sequence.Response{Body: map[string]any{
					"type": "file", "encoding": "base64", "path": "UPGRADE.md", "content": base64Encode(notes),
				}}
			}
			// Any other ref returns the opposite result, including the branch
			// head and, with --version, the newer candidate still building.
			otherNotes := "## Next\nNewer unreleased change.\n"
			if tc.refused || tc.unreadable {
				otherNotes = "## v1.3.0\nAlready renamed on main.\n"
			}
			routes["GET "+upgradePath] = []sequence.Response{file(otherNotes)}
			response := file(tc.notes)
			if tc.status != 0 {
				response = sequence.Response{Status: tc.status, Body: map[string]any{"message": "upgrade notes unavailable"}}
			}
			routes["GET "+upgradePath+"?ref="+candidate] = []sequence.Response{response}
			server, open := mockSources(t, routes)

			doc, err := runCommand(t, []string{repository}, &flag{Version: tc.version, Branch: tc.branch, DryRun: tc.dryRun}, open)

			entries := doc["repositories"].([]any)
			require.Len(t, entries, 1)
			entry := entries[0].(map[string]any)
			require.Equal(t, candidate, entry["candidate"])
			switch {
			case tc.refused || tc.unreadable:
				require.Equal(t, agentcli.ExitRed, agentcli.Exit(err), "%v", err)
				require.Equal(t, float64(agentcli.ExitRed), doc["exitCode"])
				require.Equal(t, string(agentcli.VerdictRed), doc["verdict"])
				require.Equal(t, releasepromote.StateFailed, entry["state"])
				require.Contains(t, entry["message"], "UPGRADE.md at "+candidate)
				if tc.refused {
					require.Equal(t, fmt.Sprintf("UPGRADE.md at %s still has a ## Next section: land the rename from ## Next to ## %s, then promote a new release candidate containing that change", candidate, releasepromote.StableVersion(candidate)), entry["message"])
				} else {
					require.Contains(t, entry["message"], "upgrade notes unavailable")
				}
				require.Zero(t, dispatches(server))
				require.NotContains(t, entry, "run")
			case tc.dryRun:
				require.NoError(t, err)
				require.Equal(t, releasepromote.StateWouldDispatch, entry["state"])
				require.Zero(t, dispatches(server))
			default:
				require.NoError(t, err)
				require.Equal(t, releasepromote.StateDispatched, entry["state"])
				require.Equal(t, 1, dispatches(server))
			}
			reads := 0
			for _, request := range server.Requests() {
				if request.Path == upgradePath {
					reads++
					require.Equal(t, http.MethodGet, request.Method)
					require.Equal(t, candidate, request.Query.Get("ref"))
				}
			}
			require.Equal(t, 1, reads, "read exactly the selected candidate's upgrade notes")
		})
	}
}
