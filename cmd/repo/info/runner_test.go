package info

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

// The text names the version, the caller, the identities and the modes.
func TestPrint(t *testing.T) {
	var info manager.Info
	require.NoError(t, json.Unmarshal([]byte(`{"version":"0.25.2","toolPrefix":"giantswarm-repo-manager","caller":{"login":"alice","id":7},
	  "auth":{"mode":"bearer","authorizationServer":"https://github.com/apps/giantswarm-repo-manager"},
	  "teamFiles":{"repository":"giantswarm/github","ref":"main","readable":"true"},
	  "inventory":{"identity":"app giantswarm-repo-manager-inventory (installation 1)","connected":true,"records":1790},
	  "circleci":{"source":"statuses+artifact"},"reviews":{"configured":true,"debugChannel":"bumblebee-test"},
	  "engine":{"module":"github.com/giantswarm/devctl/v8","version":"v8.82.10","package":"pkg/reposetup"},
	  "capabilities":{"modes":["commit"],"applyRefused":true,"writeTools":["set_lifecycle","transfer_repository"]}}`), &info))
	var out bytes.Buffer
	print(&out, "https://muster.example/mcp", &info)
	text := out.String()
	for _, want := range []string{
		"giantswarm-repo-manager 0.25.2 at https://muster.example/mcp (tools x_giantswarm-repo-manager_*)",
		"caller: alice (id 7), bearer through https://github.com/apps/giantswarm-repo-manager",
		"team files: giantswarm/github@main, readable true",
		"inventory: reads as app giantswarm-repo-manager-inventory (installation 1), 1790 records",
		"circleci facts: statuses+artifact",
		"reviews (Slack asks): configured, every ask and notice to #bumblebee-test",
		"engine: github.com/giantswarm/devctl/v8 v8.82.10 (pkg/reposetup)",
		"writes: modes commit; apply refused; tools set_lifecycle, transfer_repository",
	} {
		require.Contains(t, text, want)
	}
}
