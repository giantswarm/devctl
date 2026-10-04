package approvealign

import (
	"reflect"
	"testing"

	"github.com/google/go-github/v92/github"
)

func issue(repo, title, login, userType string) *github.Issue {
	return &github.Issue{
		Title:   github.Ptr(title),
		HTMLURL: github.Ptr("https://github.com/giantswarm/" + repo + "/pull/1"),
		User:    &github.User{Login: github.Ptr(login), Type: github.Ptr(userType)},
	}
}

func TestSelectTeamPRs(t *testing.T) {
	repos := map[string]bool{"flexshopper": true, "debug": true}
	issues := []*github.Issue{
		issue("flexshopper", alignTitle, "github-actions[bot]", "Bot"),
		issue("debug", alignTitle+" #2", "teams-bot", "Bot"),
		issue("flexshopper", alignTitle, "teemow", "User"),
		issue("other-team-repo", alignTitle, "github-actions[bot]", "Bot"),
		issue("debug", "chore: something else", "github-actions[bot]", "Bot"),
	}

	got := selectTeamPRs(issues, repos)

	if len(got) != 2 || got[0] != issues[0] || got[1] != issues[1] {
		t.Fatalf("selected %d PRs, want the two bot-authored Align files PRs of the team's repos", len(got))
	}
}

func TestMissingContexts(t *testing.T) {
	combined := &github.CombinedStatus{Statuses: []*github.RepoStatus{{Context: github.Ptr("ci/circleci: build")}}}
	runs := &github.ListCheckRunsResults{CheckRuns: []*github.CheckRun{{Name: github.Ptr("pre-commit")}}}

	got := missingContexts([]string{"ci/circleci: build", "pre-commit", "go-test", "gitleaks"}, combined, runs)
	want := []string{"go-test", "gitleaks"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("missing = %v, want %v", got, want)
	}

	if got := missingContexts([]string{"a"}, nil, nil); !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("nil inputs: missing = %v, want [a]", got)
	}
}

func TestFlagValidate(t *testing.T) {
	for team, wantErr := range map[string]bool{"": false, "planeteers": false, "team-planeteers": false, "a/b": true, "x.yaml": true} {
		if err := (&flag{Team: team}).Validate(); (err != nil) != wantErr {
			t.Errorf("Validate(%q) error = %v, want error %v", team, err, wantErr)
		}
	}
}
