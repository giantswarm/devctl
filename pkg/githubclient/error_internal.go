package githubclient

import "github.com/google/go-github/v62/github"

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
