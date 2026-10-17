package releasepromote

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

func TestTeamFileRepositories(t *testing.T) {
	tf, err := reposetup.ParseTeamFile("team-bumblebee", strings.NewReader(`
- name: released
  gen:
    ci:
      generate: true
- name: legacy
  gen:
    ci:
      generate: false
- name: deprecated
  lifecycle: deprecated
  gen:
    ci:
      generate: true
- name: archived
  lifecycle: archived
  gen:
    ci:
      generate: true
- name: deleted
  lifecycle: deleted
  gen:
    ci:
      generate: true
`))
	require.NoError(t, err)

	repositories, err := teamFileRepositories(tf)
	require.NoError(t, err)
	// An archived repository takes no release and a deleted one is gone
	// from GitHub: both stay in the team file only as the record.
	require.Equal(t, []string{"giantswarm/released", "giantswarm/deprecated"}, repositories)
}
