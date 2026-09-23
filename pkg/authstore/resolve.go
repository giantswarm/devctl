package authstore

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// EnvCI is the variable a CI system sets: any non-empty value means devctl
// runs unattended, reads no keychain and prints no override warning.
const EnvCI = "CI"

// SourceKeychain is [Token].Source of the devctl GitHub App login.
const SourceKeychain = "keychain"

// GitHubEnvVars are the variables [ResolveGitHub] reads a GitHub token from
// when a command names none, in this order.
var GitHubEnvVars = []string{"DEVCTL_GITHUB_TOKEN", "GITHUB_TOKEN", "OPSCTL_GITHUB_TOKEN"}

// ResolveGitHub is the GitHub token a command acts with. envVars are the
// variables to read, in order; none means [GitHubEnvVars], a command with
// --github-token-envvar passes its one name.
//
//   - The first set variable is an explicit override: its token, Source
//     "$NAME", and Warning recommending `devctl auth login --github-only` and
//     unsetting the variable; no Warning when [EnvCI] is set.
//   - Otherwise, with [EnvCI] set: [ErrAuthRequired] naming the variables. The
//     keychain is never opened.
//   - Otherwise the App login from the keychain, refreshed when expired, Source
//     [SourceKeychain]; [ErrAuthRequired] naming `devctl auth login
//     --github-only` when there is none or it cannot be refreshed.
//
// It never falls back from one source to another after a failure and never
// asks `gh auth token`. The caller prints Warning once, where it prints its
// other warnings, and returns the error unchanged: it is exit 8.
func ResolveGitHub(ctx context.Context, envVars ...string) (Token, error) {
	return resolveGitHub(ctx, func() (*Auth, error) { return Open(nil) }, githubVars(envVars))
}

// GitHubOverrideWarning is the Warning [ResolveGitHub] puts on a token from
// one of envVars (none means [GitHubEnvVars]): empty when none is set or
// [EnvCI] is set. `auth status` reports it.
func GitHubOverrideWarning(envVars ...string) string {
	token, _ := githubEnvToken(githubVars(envVars))
	return token.Warning
}

func resolveGitHub(ctx context.Context, open func() (*Auth, error), envVars []string) (Token, error) {
	if token, ok := githubEnvToken(envVars); ok {
		return token, nil
	}
	if ci() {
		return Token{}, &AuthRequiredError{
			Identity: identityGitHub,
			Cause:    fmt.Sprintf("$%s is set, so the keychain is not read, and there is no token in %s", EnvCI, variables(envVars)),
		}
	}
	a, err := open()
	if err != nil {
		return Token{}, err
	}
	return a.requireGitHub(ctx, hintLoginGitHub)
}

// githubVars is envVars, or [GitHubEnvVars] when there are none.
func githubVars(envVars []string) []string {
	if len(envVars) == 0 {
		return GitHubEnvVars
	}
	return envVars
}

// githubEnvToken is the token of the first set variable of envVars with its
// override warning outside CI.
func githubEnvToken(envVars []string) (Token, bool) {
	for _, name := range envVars {
		value := os.Getenv(name)
		if value == "" {
			continue
		}
		token := Token{Value: value, Source: "$" + name}
		if !ci() {
			token.Warning = fmt.Sprintf("the GitHub token in $%s overrides the devctl GitHub App login: run `%s` and unset %s.",
				name, hintLoginGitHub, name)
		}
		return token, true
	}
	return Token{}, false
}

func ci() bool { return os.Getenv(EnvCI) != "" }

// variables names envVars for a sentence: "$A", "$A or $B", "$A, $B or $C".
func variables(envVars []string) string {
	names := make([]string, len(envVars))
	for i, name := range envVars {
		names[i] = "$" + name
	}
	if len(names) == 1 {
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}
