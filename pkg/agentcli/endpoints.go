package agentcli

import (
	"os"
	"strings"
)

// The environment variables that point an agent-facing command at another
// site or at a test double. Every default is the production endpoint.
const (
	// EnvGitHubAPIURL is the GitHub REST API (https://api.github.com).
	EnvGitHubAPIURL = "DEVCTL_GITHUB_API_URL"
	// EnvGitHubOAuthURL is the host of GitHub's device-flow endpoints
	// (https://github.com).
	EnvGitHubOAuthURL = "DEVCTL_GITHUB_OAUTH_URL"
	// EnvCircleCIAPIURL is the CircleCI API v2 (https://circleci.com/api/v2).
	EnvCircleCIAPIURL = "DEVCTL_CIRCLECI_API_URL"
	// EnvCircleCIOAuthURL is CircleCI's OAuth issuer (https://app.circleci.com).
	EnvCircleCIOAuthURL = "DEVCTL_CIRCLECI_OAUTH_URL"
	// EnvRegistryPublic is the public registry, probed anonymously.
	EnvRegistryPublic = "DEVCTL_REGISTRY_PUBLIC"
	// EnvRegistryPrivate is the private registry, read with the docker keychain.
	EnvRegistryPrivate = "DEVCTL_REGISTRY_PRIVATE"
	// EnvRegistryInsecure set to 1 talks plain HTTP to the registries (tests only).
	EnvRegistryInsecure = "DEVCTL_REGISTRY_INSECURE"
	// EnvKeyringFile names a 0600 JSON file that replaces the OS keychain
	// (tests only).
	EnvKeyringFile = "DEVCTL_KEYRING_FILE"
)

// Endpoints is where the agent-facing commands talk to.
type Endpoints struct {
	GitHubAPIURL     string
	GitHubOAuthURL   string
	CircleCIAPIURL   string
	CircleCIOAuthURL string
	RegistryPublic   string
	RegistryPrivate  string
	RegistryInsecure bool
	// KeyringFile is empty for the OS keychain.
	KeyringFile string
}

// DefaultEndpoints are the production endpoints.
func DefaultEndpoints() Endpoints {
	return Endpoints{
		GitHubAPIURL:     "https://api.github.com",
		GitHubOAuthURL:   "https://github.com",
		CircleCIAPIURL:   "https://circleci.com/api/v2",
		CircleCIOAuthURL: "https://app.circleci.com",
		RegistryPublic:   "gsoci.azurecr.io",
		RegistryPrivate:  "gsociprivate.azurecr.io",
	}
}

// EndpointsFromEnv are the defaults with every set variable applied. URLs
// lose their trailing slash.
func EndpointsFromEnv() Endpoints {
	e := DefaultEndpoints()
	override := func(key string, target *string) {
		if v, ok := os.LookupEnv(key); ok && v != "" {
			*target = strings.TrimRight(v, "/")
		}
	}
	override(EnvGitHubAPIURL, &e.GitHubAPIURL)
	override(EnvGitHubOAuthURL, &e.GitHubOAuthURL)
	override(EnvCircleCIAPIURL, &e.CircleCIAPIURL)
	override(EnvCircleCIOAuthURL, &e.CircleCIOAuthURL)
	override(EnvRegistryPublic, &e.RegistryPublic)
	override(EnvRegistryPrivate, &e.RegistryPrivate)
	e.RegistryInsecure = os.Getenv(EnvRegistryInsecure) == "1"
	e.KeyringFile = os.Getenv(EnvKeyringFile)
	return e
}
