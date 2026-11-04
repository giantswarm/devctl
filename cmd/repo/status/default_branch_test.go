package status

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

// Catalogs and customer repositories keep their current branch even when the
// declaration names a different one. Other repositories follow the entry.
func TestRunTextDefaultBranchPolicy(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fields  string
		reality *manager.Reality
		want    string
	}{
		{
			name:    "catalog ignores declared branch",
			fields:  "  componentType: appcatalog\n  defaultBranch: main\n",
			reality: &manager.Reality{DefaultBranch: "master"},
			want:    "default branch master, flavours none",
		},
		{
			name:    "customer ignores declared branch",
			fields:  "  componentType: service\n  defaultBranch: main\n  gen:\n    flavours: [customer]\n    language: generic\n",
			reality: &manager.Reality{DefaultBranch: "production"},
			want:    "default branch production, flavours customer",
		},
		{
			name:    "customer without declared branch",
			fields:  "  componentType: service\n  gen:\n    flavours: [customer]\n    language: generic\n",
			reality: &manager.Reality{DefaultBranch: "production"},
			want:    "default branch production, flavours customer",
		},
		{
			name:    "service follows declared branch",
			fields:  "  componentType: service\n  defaultBranch: release\n",
			reality: &manager.Reality{DefaultBranch: "master"},
			want:    "default branch release, flavours none",
		},
		{
			name:    "service follows baseline",
			fields:  "  componentType: service\n",
			reality: &manager.Reality{DefaultBranch: "master"},
			want:    "default branch main, flavours none",
		},
		{
			name:   "catalog without reality retains declaration",
			fields: "  componentType: appcatalog\n  defaultBranch: release\n",
			want:   "default branch release, flavours none",
		},
		{
			name:    "catalog without observed branch retains declaration",
			fields:  "  componentType: appcatalog\n  defaultBranch: release\n",
			reality: &manager.Reality{},
			want:    "default branch release, flavours none",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := json.Marshal(manager.Record{
				Repository:  "giantswarm/my-service",
				Declaration: &manager.Declaration{Accepted: true, Entry: "- name: my-service\n" + tc.fields},
				Reality:     tc.reality,
				Setup:       manager.Setup{Checks: result()},
			})
			require.NoError(t, err)
			r, _, out := newRunner(string(payload), client.OutputText)
			require.NoError(t, r.run(context.Background(), "my-service"))
			require.Contains(t, out.String(), "\n"+tc.want+"\n")
		})
	}
}
