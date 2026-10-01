// Package auth resolves the GitHub token the repo commands act with: the
// person's own, so a pull request they open is theirs.
package auth

import (
	"context"
	"os"
	"os/exec"
	"strings"

	"github.com/giantswarm/microerror"
)

// GitHubToken returns the token in the named environment variable, else
// the one the person's gh CLI is logged in with (`gh auth token`), and says
// where it came from. No token anywhere is [IsNoToken].
func GitHubToken(ctx context.Context, envVar string) (token, source string, err error) {
	if token := os.Getenv(envVar); token != "" {
		return token, "$" + envVar, nil
	}

	out, err := exec.CommandContext(ctx, "gh", "auth", "token").Output() // #nosec G204 -- fixed binary and arguments
	if err == nil {
		if token := strings.TrimSpace(string(out)); token != "" {
			return token, "gh auth token", nil
		}
	}

	return "", "", microerror.Maskf(noTokenError, "no GitHub token: set $%s or log in with `gh auth login`", envVar)
}

var noTokenError = &microerror.Error{
	Kind: "noTokenError",
}

// IsNoToken asserts noTokenError.
func IsNoToken(err error) bool {
	return microerror.Cause(err) == noTokenError
}
