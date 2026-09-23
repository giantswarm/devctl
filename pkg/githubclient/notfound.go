package githubclient

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/google/go-github/v92/github"
)

// ExplainNotFound is err with hint added when err is GitHub's answer that a
// repository or resource does not exist: an API call's 404 or git's
// "repository not found". GitHub gives that answer, not a 403, for a private
// repository the token cannot read, so hint names what the token reaches
// (authstore.GitHubNotFoundHint). Any other error, and any error when hint is
// empty, is returned unchanged.
func ExplainNotFound(err error, hint string) error {
	if hint == "" || !isNotFoundAnswer(err) {
		return err
	}
	return fmt.Errorf("%w; %s", err, hint)
}

// isNotFoundAnswer reports whether err, or an error it wraps, is GitHub's 404
// or git's repository not found.
func isNotFoundAnswer(err error) bool {
	var response *github.ErrorResponse
	if errors.As(err, &response) && response.Response != nil && response.Response.StatusCode == http.StatusNotFound {
		return true
	}
	return errors.Is(err, transport.ErrRepositoryNotFound)
}
