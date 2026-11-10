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

	// Git's repository-relative prefix uses URL path separators and is not
	// affected by the logical PWD of a symlinked checkout.
	prefixCmd := exec.Command("git", "rev-parse", "--show-prefix")
	gitPrefix, err := prefixCmd.CombinedOutput()
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
	shaCmd := exec.Command("git", "rev-list", "-1", "HEAD", "--", filename)
	sha, err := shaCmd.CombinedOutput()
	if err != nil {
		log.Fatal(err)
	}

	templatePath := strings.TrimSpace(string(gitPrefix)) + filename

	if err := os.WriteFile(filename+".sha", []byte(fmt.Sprintf("https://github.com/giantswarm/devctl/blob/%s/%s", strings.TrimSpace(string(sha)), templatePath)), 0666); err != nil {
		log.Fatal(err)
	}
}
