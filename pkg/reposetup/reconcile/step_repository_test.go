package reconcile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_openPullRequestsMessage(t *testing.T) {
	require.Equal(t,
		"1 pull request is open in giantswarm/sample and stays open, read-only, in the archive: #7",
		openPullRequestsMessage("giantswarm/sample", []string{"#7"}))
	require.Equal(t,
		"2 pull requests are open in giantswarm/sample and stay open, read-only, in the archive: #7, #9",
		openPullRequestsMessage("giantswarm/sample", []string{"#7", "#9"}))
}
