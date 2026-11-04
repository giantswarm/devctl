package githubclient

import (
	"strings"

	"github.com/google/go-github/v93/github"
)

// GitHub's CLI also recognizes the first message when an update conflicts:
// https://github.com/cli/cli/blob/eeb44bc432c0107de002dadd34a61b4e2bd15882/pkg/cmd/pr/update-branch/update_branch.go
// Match explicit conflict reasons, not arbitrary GraphQL failures.
func isBranchUpdateConflictMessage(message string) bool {
	message = strings.TrimSuffix(strings.ToLower(message), " (updatepullrequestbranch)")
	return message == "merge conflict between base and head" || message == "rebase failed due to conflicts"
}

func isGithub404(err error) bool {
	if err == nil {
		return false
	}

	errResponse, ok := err.(*github.ErrorResponse)
	if !ok {
		return false
	}

	if errResponse.Response == nil {
		return false
	}

	return errResponse.Response.StatusCode == 404
}
