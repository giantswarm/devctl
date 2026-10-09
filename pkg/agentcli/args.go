package agentcli

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseRepository reads "<owner/repo>".
func ParseRepository(arg string) (owner, repo string, err error) {
	owner, repo, ok := strings.Cut(arg, "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return "", "", fmt.Errorf("the repository is <owner/repo>, got %q", arg)
	}
	return owner, repo, nil
}

// ParsePullRequest reads the arguments "<owner/repo> <number>" of command.
func ParsePullRequest(command string, args []string) (owner, repo string, number int, err error) {
	if len(args) != 2 {
		return "", "", 0, fmt.Errorf("usage: devctl %s <owner/repo> <number>, got %d argument(s)", command, len(args))
	}
	if owner, repo, err = ParseRepository(args[0]); err != nil {
		return "", "", 0, err
	}
	number, err = strconv.Atoi(args[1])
	if err != nil || number <= 0 {
		return "", "", 0, fmt.Errorf("the pull request number is a positive integer, got %q", args[1])
	}
	return owner, repo, number, nil
}
