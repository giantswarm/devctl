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

	shaCmd := exec.Command("git", "rev-list", "--all", "-1", "--", fmt.Sprintf("%s/%s", currentWorkingDirectory, filename))
	sha, err := shaCmd.CombinedOutput()
	if err != nil {
		log.Fatal(err)
	}

	strippedPath := strings.TrimPrefix(currentWorkingDirectory, strings.TrimSpace(string(gitRoot)))

	if err := os.WriteFile(filename+".sha", []byte(fmt.Sprintf("https://github.com/giantswarm/devctl/blob/%s%s/%s", strings.TrimSpace(string(sha)), strippedPath, filename)), 0666); err != nil {
		log.Fatal(err)
	}
}
