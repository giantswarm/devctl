package reservation

import "testing"

func TestSplitPullRequest(t *testing.T) {
	owner, repo, number, err := SplitPullRequest("giantswarm/hello-world.go#123")
	if err != nil || owner != "giantswarm" || repo != "hello-world.go" || number != 123 {
		t.Fatalf("got %q %q %d %v", owner, repo, number, err)
	}

	for _, in := range []string{
		"", "giantswarm/hello-world", "giantswarm#1", "/repo#1", "owner/#1",
		"a/b/c#1", "owner/repo#0", "owner/repo#-1", "owner/repo#01", "owner/repo#1x",
		"owner/..#1", "owner/.#1", "own er/repo#1", "owner/repo#99999999999999999999",
	} {
		if _, _, _, err := SplitPullRequest(in); err == nil {
			t.Errorf("SplitPullRequest(%q) accepted", in)
		}
	}
}
