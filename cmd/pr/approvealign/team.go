package approvealign

import (
	"context"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v92/github"

	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

const (
	// alignTitle is the title every Align files PR carries.
	alignTitle = "chore: align files according to platform standards"

	teamFileOwner = "giantswarm"
	teamFileRepo  = "github"
)

// teamRepositories returns the names of the repositories declared in
// repositories/team-<team>.yaml of giantswarm/github.
func teamRepositories(ctx context.Context, client *github.Client, team string) (map[string]bool, error) {
	team = strings.TrimPrefix(team, "team-")
	path := "repositories/team-" + team + ".yaml"

	file, _, _, err := client.Repositories.GetContents(ctx, teamFileOwner, teamFileRepo, path, nil)
	if err != nil {
		return nil, microerror.Maskf(executionFailedError, "failed to read %s of %s/%s: %v", path, teamFileOwner, teamFileRepo, err)
	}
	content, err := file.GetContent()
	if err != nil {
		return nil, microerror.Mask(err)
	}

	tf, err := reposetup.ParseTeamFile("team-"+team, strings.NewReader(content))
	if err != nil {
		return nil, microerror.Mask(err)
	}

	repos := map[string]bool{}
	for _, e := range tf.Entries {
		if e.Name != "" {
			repos[e.Name] = true
		}
	}
	return repos, nil
}

// selectTeamPRs keeps the Align files PRs of the given repositories that a
// bot opened; a person's PR is never selected.
func selectTeamPRs(issues []*github.Issue, repos map[string]bool) []*github.Issue {
	var selected []*github.Issue
	for _, issue := range issues {
		if !isBot(issue.GetUser()) || !strings.HasPrefix(issue.GetTitle(), alignTitle) {
			continue
		}
		if !repos[repoOfIssue(issue)] {
			continue
		}
		selected = append(selected, issue)
	}
	return selected
}

func isBot(u *github.User) bool {
	return u != nil && (u.GetType() == "Bot" || strings.HasSuffix(u.GetLogin(), "[bot]"))
}

func repoOfIssue(issue *github.Issue) string {
	// https://github.com/<owner>/<repo>/pull/<n>
	parts := strings.Split(strings.TrimPrefix(issue.GetHTMLURL(), "https://github.com/"), "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

// missingContexts returns the required status contexts that no check run and
// no commit status of the head produces, in the order required.
func missingContexts(required []string, combined *github.CombinedStatus, runs *github.ListCheckRunsResults) []string {
	produced := map[string]bool{}
	if combined != nil {
		for _, s := range combined.Statuses {
			produced[s.GetContext()] = true
		}
	}
	if runs != nil {
		for _, r := range runs.CheckRuns {
			produced[r.GetName()] = true
		}
	}

	var missing []string
	for _, c := range required {
		if !produced[c] {
			missing = append(missing, c)
		}
	}
	return missing
}

// requiredContexts returns the status contexts the base branch requires,
// from its active rulesets.
func requiredContexts(ctx context.Context, client *github.Client, owner, repo, branch string) ([]string, error) {
	rules, _, err := client.Repositories.ListRulesForBranch(ctx, owner, repo, branch, nil)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	var contexts []string
	if rules == nil {
		return contexts, nil
	}
	for _, rule := range rules.RequiredStatusChecks {
		if rule == nil {
			continue
		}
		for _, c := range rule.Parameters.RequiredStatusChecks {
			if c != nil {
				contexts = append(contexts, c.Context)
			}
		}
	}
	return contexts, nil
}
