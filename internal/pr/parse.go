package pr

import (
	"fmt"
	"strings"
)

// ParseRepoFromURL extracts owner and repo name from GitHub PR URL.
// Example: "https://github.com/giantswarm/backstage/pull/1033" -> "giantswarm", "backstage"
func ParseRepoFromURL(url string) (string, string, error) {
	parts := strings.Split(url, "/")
	if len(parts) < 5 {
		return "", "", fmt.Errorf("invalid GitHub URL format: %s", url)
	}
	// URL format: https://github.com/{owner}/{repo}/pull/{number}
	owner := parts[3]
	repo := parts[4]
	return owner, repo, nil
}

