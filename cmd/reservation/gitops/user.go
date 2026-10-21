package gitops

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"

	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

// UserHelp is the --user help text shared by every reservation command.
func UserHelp(what string) string {
	return "GitHub login " + what + ". Empty takes the login of the GitHub token devctl acts with. The commit author is your git identity, not this."
}

// User returns user when it is set, and otherwise the login of the GitHub
// token devctl acts with. A GitHub App installation token has no login, so a
// caller that acts with one passes --user.
func User(ctx context.Context, user string, stderr io.Writer) (string, error) {
	if user != "" {
		return user, nil
	}

	token, err := authstore.ResolveGitHub(ctx)
	if errors.Is(err, authstore.ErrAuthRequired) {
		_, _ = fmt.Fprintln(stderr, "No --user given, and no GitHub token to take the login from. Pass --user.")
	}
	if err != nil {
		return "", err
	}
	token.WarnOnce(stderr)

	client, err := githubclient.New(githubclient.Config{
		Logger:      logrus.StandardLogger(),
		AccessToken: token.Value,
	})
	if err != nil {
		return "", microerror.Mask(err)
	}
	login, err := client.CurrentLogin(ctx)
	if err != nil {
		return "", microerror.Maskf(invalidFlagError, "no --user given, and cannot read the login of the GitHub token: %v. Pass --user", err)
	}
	if err := reservation.ValidateUser(login); err != nil {
		return "", microerror.Maskf(invalidFlagError, "the GitHub token's login: %v. Pass --user", err)
	}

	return login, nil
}
