package reconcile

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/go-github/v92/github"
)

const (
	// renovateLogin is the login of the Renovate GitHub App: the creator of
	// the Dependency Dashboard issue and of the dependency pull requests, the
	// author of their squash commits.
	renovateLogin = "renovate[bot]"
	// renovateDashboardTitle is the issue Renovate keeps on every repository
	// it scans, unless the configuration turns the dashboard off.
	renovateDashboardTitle = "Dependency Dashboard"
)

// renovateConfigPaths are the configuration files Renovate reads, in its
// order of precedence.
var renovateConfigPaths = []string{"renovate.json5", "renovate.json", ".github/renovate.json5", ".github/renovate.json", ".renovaterc", ".renovaterc.json"}

// renovateDisabled matches a top-level `enabled: false` of the configuration,
// JSON or JSON5; one nested in packageRules is indented further.
var renovateDisabled = regexp.MustCompile(`(?m)^\s{0,2}["']?enabled["']?\s*:\s*false`)

// installationRepositories is a page of GET /user/installations/{id}/repositories.
type installationRepositories struct {
	TotalCount          int    `json:"total_count"`
	RepositorySelection string `json:"repository_selection"`
	Repositories        []struct {
		FullName string `json:"full_name"`
	} `json:"repositories"`
}

// stepRenovate checks that Renovate scans the repository, from the
// repository's own evidence: a configuration file, and a trace of a run —
// the Dependency Dashboard issue, else a pull request or a commit of
// Renovate's. Both present is ok, as is a configuration that disables
// Renovate. The Renovate installation covers every repository of the
// organization, so what is missing is reported for a person and never
// repaired: a configuration to add, or a run that has not happened yet or a
// configuration Renovate refuses. The installation's repository list, which
// only an organization owner's token reads, is detail in the summary and
// never decides the verdict.
func (r *Runner) stepRenovate(ctx context.Context, s *run, sr *StepResult) error {
	config, disabled, err := r.renovateConfig(ctx, s)
	if err != nil {
		return err
	}
	var trace string
	if config != "" && !disabled {
		trace, err = r.renovateTrace(ctx, s)
		if err != nil {
			return err
		}
	}
	switch {
	case config == "":
		s.report(sr, FindingRenovateNotScanned,
			fmt.Sprintf("%s has no Renovate configuration: Renovate opens no dependency updates", s.slug()),
			"add renovate.json5 (`devctl gen renovate` writes it; align-files renders it for repositories on devctl-generated CI) or merge Renovate's onboarding pull request; the installation covers all repositories, there is nothing to add there")
	case disabled:
		sr.Summary = config + " disables Renovate"
	case trace == "":
		s.report(sr, FindingRenovateNotScanned,
			fmt.Sprintf("%s has %s but no trace of a Renovate run: no %s issue, no pull request and no commit of %s on %s", s.slug(), config, renovateDashboardTitle, renovateLogin, s.branch()),
			fmt.Sprintf("the Renovate installation covers all repositories of the organization, so Renovate has not run yet (the %s issue follows its first run) or refuses the configuration: check the repository's job log on the Renovate dashboard", renovateDashboardTitle))
	default:
		sr.Summary = config + "; " + trace
	}
	if detail := r.renovateInstallation(ctx, s); detail != "" {
		if sr.Summary != "" {
			sr.Summary += "; "
		}
		sr.Summary += detail
	}
	return nil
}

// renovateConfig finds the Renovate configuration on the default branch: its
// path ("" when there is none) and whether it disables Renovate.
func (r *Runner) renovateConfig(ctx context.Context, s *run) (string, bool, error) {
	for _, path := range renovateConfigPaths {
		content, found, err := r.fileContent(ctx, s.owner, s.name, path, s.branch())
		if err != nil {
			return "", false, err
		}
		if found {
			return path, renovateDisabled.Match(content), nil
		}
	}
	return "", false, nil
}

// renovateTrace is the trace of a Renovate run on the repository, or "": the
// Dependency Dashboard issue, else a pull request of Renovate's (open, merged
// or closed), else a commit of Renovate's among the latest hundred on the
// default branch — a squash merge keeps Renovate as the author.
func (r *Runner) renovateTrace(ctx context.Context, s *run) (string, error) {
	items, _, err := r.GitHub.Issues.ListByRepo(ctx, s.owner, s.name, &github.IssueListByRepoOptions{
		Creator: renovateLogin, State: "all", Sort: "created", Direction: "asc",
		ListOptions: github.ListOptions{PerPage: 100},
	})
	if err != nil {
		return "", err
	}
	var pull *github.Issue
	for _, item := range items {
		switch {
		case item.IsPullRequest():
			if pull == nil {
				pull = item
			}
		case strings.EqualFold(item.GetTitle(), renovateDashboardTitle):
			return fmt.Sprintf("%s issue #%d", renovateDashboardTitle, item.GetNumber()), nil
		}
	}
	if pull != nil {
		return fmt.Sprintf("Renovate pull request #%d", pull.GetNumber()), nil
	}
	commits, _, err := r.GitHub.Repositories.ListCommits(ctx, s.owner, s.name, &github.CommitsListOptions{
		SHA:         s.branch(),
		ListOptions: github.ListOptions{PerPage: 100},
	})
	if err != nil {
		return "", err
	}
	for _, c := range commits {
		if isRenovate(c.GetAuthor().GetLogin(), c.GetCommit().GetAuthor().GetName(), c.GetCommit().GetAuthor().GetEmail()) {
			return fmt.Sprintf("Renovate commit %.7s on %s", c.GetSHA(), s.branch()), nil
		}
	}
	return "", nil
}

// isRenovate says whether a login, an author name or an email is Renovate's.
func isRenovate(login, name, email string) bool {
	return strings.Contains(strings.ToLower(login+" "+name+" "+email), "renovate")
}

// renovateInstallation says whether the baseline's Renovate installation
// covers the repository, as detail for the summary; "" when the baseline
// names no installation or the token cannot read it. Listing an
// installation's repositories takes an organization owner's token: a GitHub
// App token, which the reconciler workflow runs under, is answered 403.
func (r *Runner) renovateInstallation(ctx context.Context, s *run) string {
	id := s.baseline.RenovateInstallationID
	if id == 0 {
		return ""
	}
	for page := 1; ; page++ {
		req, err := r.GitHub.NewRequest(ctx, "GET", fmt.Sprintf("user/installations/%d/repositories?per_page=100&page=%d", id, page), nil)
		if err != nil {
			return ""
		}
		var repos installationRepositories
		resp, err := r.GitHub.Do(req, &repos)
		if err != nil {
			if !isForbiddenOrNotFound(resp) {
				fmt.Fprintf(s.log, "%s/%s renovate: reading the installation's repositories: %v\n", s.owner, s.name, err)
			}
			return ""
		}
		if repos.RepositorySelection == "all" {
			return "the installation covers all repositories"
		}
		for _, repo := range repos.Repositories {
			if strings.EqualFold(repo.FullName, s.slug()) {
				return "the installation lists the repository"
			}
		}
		if resp.NextPage == 0 {
			return "the installation does not list the repository"
		}
	}
}

// isForbiddenOrNotFound says whether a go-github call answered 403 or 404.
func isForbiddenOrNotFound(resp *github.Response) bool {
	return resp != nil && resp.Response != nil && (resp.StatusCode == 403 || resp.StatusCode == 404)
}
