//go:build ignore

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

func main() {
	filename := os.Args[1]

	currentWorkingDirectory, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	rootCmd := exec.Command("git", "rev-parse", "--show-toplevel")
	gitRoot, err := rootCmd.CombinedOutput()
	if err != nil {
		log.Fatal(err)
	}

	// Scope the lookup to the current HEAD's history, not `--all`. `git rev-list --all`
	// spans every ref in the checkout (including unmerged origin/renovate/* branches), so
	// the "last commit that touched this template" could resolve to a commit that only
	// exists on an open PR branch. That made the embedded provenance SHA non-deterministic:
	// it churned release-to-release with no template content change, and the align-files
	// automation propagated each flip as a no-op PR to every consuming repo. Restricting to
	// HEAD resolves to the last commit reachable from the build's checkout (the release tag
	// on tag builds) that touched the template, so the SHA only changes when the template does.
	shaCmd := exec.Command("git", "rev-list", "-1", "HEAD", "--", fmt.Sprintf("%s/%s", currentWorkingDirectory, filename))
	sha, err := shaCmd.CombinedOutput()
	if err != nil {
		log.Fatal(err)
	}

	strippedPath := strings.TrimPrefix(currentWorkingDirectory, strings.TrimSpace(string(gitRoot)))

	if err := os.WriteFile(filename+".sha", []byte(fmt.Sprintf("https://github.com/giantswarm/devctl/blob/%s%s/%s", strings.TrimSpace(string(sha)), strippedPath, filename)), 0666); err != nil {
		log.Fatal(err)
	}
}
