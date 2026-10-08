package gitops

import (
	"strings"
	"testing"

	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

// TestExplainNamesTheFlag checks that a clone of the wrong repo is reported
// against --gitops-repo, and that a --repo-dir checkout keeps the error that
// names its directory.
func TestExplainNamesTheFlag(t *testing.T) {
	_, err := reservation.List(reservation.ListRequest{RepoDir: t.TempDir(), Cluster: "graveler"})
	if !reservation.IsNotGitOpsRepo(err) {
		t.Fatalf("expected a not-a-GitOps-repo error, got %v", err)
	}

	explained := (&Flags{Repo: "giantswarm/wrong"}).Explain(err)
	if !IsInvalidFlag(explained) || !strings.Contains(explained.Error(), "--gitops-repo giantswarm/wrong") {
		t.Errorf("clone: got %v", explained)
	}
	if got := (&Flags{Repo: DefaultRepo, RepoDir: "/somewhere"}).Explain(err); got != err {
		t.Errorf("--repo-dir: got %v, want the error unchanged", got)
	}
	other := microerror.Maskf(&microerror.Error{Kind: "other"}, "other")
	if got := (&Flags{Repo: DefaultRepo}).Explain(other); got != other {
		t.Errorf("other error: got %v, want it unchanged", got)
	}
}
