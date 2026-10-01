package checks

import (
	"testing"

	"github.com/google/go-github/v92/github"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/reposetup/reconcile"
)

func TestChecksBaseline(t *testing.T) {
	checks := []*github.RequiredStatusCheck{{Context: "ci/circleci: go-build"}}
	protection := &github.Protection{
		RequiredStatusChecks:       &github.RequiredStatusChecks{Strict: false, Checks: &checks},
		RequiredPullRequestReviews: &github.PullRequestReviewsEnforcement{RequiredApprovingReviewCount: 2},
		EnforceAdmins:              &github.AdminEnforcement{Enabled: false},
	}

	b := checksBaseline(protection, []string{"pre-commit"}, []string{"ci/circleci: build-chart"}, []string{"create-release / Gather facts"})

	// The branch's own settings are kept, so the step changes checks alone.
	require.Equal(t, 2, b.RequiredReviews)
	require.False(t, b.EnforceAdmins)
	require.False(t, b.StrictChecks)
	require.Equal(t, []string{"pre-commit"}, b.RequiredChecks)
	require.Equal(t, []string{"ci/circleci: build-chart"}, b.RequiredChecksIfReported)
	// --remove is an anchored pattern on top of the company's ignored ones.
	require.Contains(t, b.IgnoredChecks, `^create-release / Gather facts$`)
	require.Subset(t, b.IgnoredChecks, reconcile.DefaultBaseline().IgnoredChecks)

	// Without required checks configured the strict default stands.
	b = checksBaseline(&github.Protection{}, nil, nil, nil)
	require.Equal(t, reconcile.DefaultBaseline().StrictChecks, b.StrictChecks)
	require.Equal(t, 0, b.RequiredReviews)
}
