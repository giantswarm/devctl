package reposetup

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v92/github"

	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

// Verdict is the outcome of a repository name check on GitHub.
type Verdict string

const (
	// VerdictFree: no repository of that name, no redirect to another one.
	VerdictFree Verdict = "free"
	// VerdictTaken: a repository exists under the name, or the name redirects
	// to a renamed repository.
	VerdictTaken Verdict = "taken"
	// VerdictUnchecked: the name was not checked (no GitHub client, or the
	// name is invalid anyway).
	VerdictUnchecked Verdict = "unchecked"
)

// NameCheck is the verdict of the name check with the reason.
type NameCheck struct {
	Verdict Verdict `json:"verdict"`
	Detail  string  `json:"detail,omitempty"`
}

// NameChecker says whether owner/name is free on GitHub.
type NameChecker interface {
	CheckName(ctx context.Context, owner, name string) (NameCheck, error)
}

// RepositoryGetter reads one repository. *githubclient.Client is one; a
// missing repository is reported through githubclient.IsNotFound or a
// go-github 404.
type RepositoryGetter interface {
	GetRepository(ctx context.Context, owner, repo string) (*github.Repository, error)
}

// GitHubNameChecker checks names against GitHub. GitHub answers a request
// for a renamed repository with a redirect the client follows, so the
// repository that comes back under a different full name is the redirect
// — and the old name is taken as much as an existing one.
type GitHubNameChecker struct {
	Repositories RepositoryGetter
}

// CheckName implements [NameChecker].
func (c GitHubNameChecker) CheckName(ctx context.Context, owner, name string) (NameCheck, error) {
	if c.Repositories == nil {
		return NameCheck{}, microerror.Maskf(invalidConfigError, "%T.Repositories must not be nil", c)
	}

	repo, err := c.Repositories.GetRepository(ctx, owner, name)
	switch {
	case isNotFound(err):
		return NameCheck{Verdict: VerdictFree, Detail: fmt.Sprintf("no repository %s/%s on GitHub", owner, name)}, nil
	case err != nil:
		return NameCheck{}, microerror.Mask(err)
	}

	requested := owner + "/" + name
	if full := repo.GetFullName(); full != "" && !strings.EqualFold(full, requested) {
		return NameCheck{Verdict: VerdictTaken, Detail: fmt.Sprintf("%s redirects to %s: the name belongs to a renamed repository", requested, full)}, nil
	}

	detail := fmt.Sprintf("repository %s exists", requested)
	if repo.GetArchived() {
		detail += " (archived)"
	}
	return NameCheck{Verdict: VerdictTaken, Detail: detail}, nil
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	if githubclient.IsNotFound(err) {
		return true
	}
	var ghErr *github.ErrorResponse
	return errors.As(err, &ghErr) && ghErr.Response != nil && ghErr.Response.StatusCode == http.StatusNotFound
}

// Repository names are lowercase: GitHub treats them case-insensitively and
// the organisation's repositories, catalog entities and chart names are
// lowercase throughout. A chart repository is named after its chart, so it
// is a DNS label as Helm requires.
var (
	repositoryNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	chartNamePattern      = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
)

const (
	repositoryNameRule = "must be lowercase letters, digits, dots, dashes or underscores, starting with a letter or digit"
	chartNameRule      = "must be lowercase letters, digits and dashes, starting and ending with a letter or digit (the chart is named after the repository)"
	chartSuffix        = "-app"
)
