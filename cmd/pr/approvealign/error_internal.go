package approvealign

import (
	"errors"

	"github.com/google/go-github/v93/github"
)

// isGitHubAccepted identifies a write scheduled for background processing.
func isGitHubAccepted(err error) bool {
	var accepted *github.AcceptedError
	return errors.As(err, &accepted)
}
