package authexec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

// SourceGHLogin is [authstore.Token].Source of the person's own gh login.
const SourceGHLogin = "gh login"

// ghTokenVars are the variables gh prefers over its stored login: the person's
// own login is read with none of them set.
var ghTokenVars = []string{EnvToken, "GITHUB_TOKEN"}

// RepositoryToken is the GitHub token a command acts with on owner's
// repositories: the App login (app) for an owner in [AppOwners], where the
// App is installed, and the person's own gh login (person) for every other
// owner, which the App cannot reach.
func RepositoryToken(ctx context.Context, owner string, app, person func(context.Context) (authstore.Token, error)) (authstore.Token, error) {
	if AppOwners[owner] {
		return app(ctx)
	}
	return person(ctx)
}

// PersonGitHub is the token of the person's own gh login: `gh auth token` of
// the real gh, never a gh link to devctl, and with neither $GH_TOKEN nor
// $GITHUB_TOKEN in its environment, so gh answers its stored login. No gh or
// no login is exit 8 naming `gh auth login`.
func PersonGitHub(ctx context.Context) (authstore.Token, error) {
	self, _ := os.Executable()
	return personGitHub(ctx, os.Getenv("PATH"), self, os.Environ(), commandOutput)
}

func personGitHub(ctx context.Context, path, self string, environ []string, output func(ctx context.Context, path string, args, env []string) ([]byte, error)) (authstore.Token, error) {
	gh, err := realGH(path, self)
	if err != nil {
		return authstore.Token{}, personAuthRequired(err.Error())
	}
	env := environ
	for _, name := range ghTokenVars {
		env = without(env, name)
	}
	out, err := output(ctx, gh, []string{"auth", "token", "--hostname", "github.com"}, env)
	token := strings.TrimSpace(string(out))
	if err != nil || token == "" {
		return authstore.Token{}, personAuthRequired("gh has no login for github.com")
	}
	return authstore.Token{Value: token, Source: SourceGHLogin}, nil
}

// realGH is the first gh on path that is no devctl: neither self nor a gh
// link to another devctl, which would answer with the App login.
func realGH(path, self string) (string, error) {
	for _, dir := range filepath.SplitList(path) {
		gh, err := LookPath("gh", dir, self)
		if err != nil || strings.TrimSuffix(filepath.Base(resolve(gh)), ".exe") == "devctl" {
			continue
		}
		return gh, nil
	}
	return "", errors.New("gh: not found on PATH (gh links to devctl skipped)")
}

func personAuthRequired(cause string) error {
	return &authstore.AuthRequiredError{
		Identity: "GitHub (gh login, for a repository outside " + appOwnerList() + ")",
		Cause:    cause,
		Hint:     "gh auth login",
	}
}

func commandOutput(ctx context.Context, path string, args, env []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, path, args...) // #nosec G204 G702 -- the real gh with fixed arguments
	cmd.Env = env
	return cmd.Output()
}

// The identities a command's document names ([Identity]).
const (
	// IdentityApp is the devctl GitHub App login.
	IdentityApp = "app"
	// IdentityGH is the person's own gh login.
	IdentityGH = "gh"
)

// Identity names the identity token acts as in a command's document.
func Identity(token authstore.Token) string {
	if token.Source == SourceGHLogin {
		return IdentityGH
	}
	return IdentityApp
}

// NotFoundHint is the sentence a command acting through [RepositoryToken]
// adds to GitHub's 404: what the identity it used reaches and what is
// missing.
func NotFoundHint(token authstore.Token, owner string) string {
	if token.Source == SourceGHLogin {
		return fmt.Sprintf("the devctl GitHub App is installed on %s only, so %s's repositories are read with your own gh login, "+
			"which cannot read this one: it needs read access, or `gh auth login` as an account that has it.", appOwnerList(), owner)
	}
	return authstore.GitHubAppOnlyNotFoundHint(token)
}

// appOwnerList names [AppOwners] for a sentence.
func appOwnerList() string {
	owners := make([]string, 0, len(AppOwners))
	for owner := range AppOwners {
		owners = append(owners, owner)
	}
	sort.Strings(owners)
	return strings.Join(owners, ", ")
}
