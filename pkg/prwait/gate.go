package prwait

import "github.com/google/go-github/v92/github"

// NotApplicable is the rule Wait applies before its first poll, for a caller
// that reads the pull request itself first (pr merge, whose refusals come
// before the wait): exit 3 for a pull request no wait can turn green, nil
// otherwise. The verdict is the one Wait would give.
func NotApplicable(pr *github.PullRequest) error {
	return notApplicable(pr)
}

// IsFork says whether the head lives in another repository than the base.
func IsFork(pr *github.PullRequest) bool {
	return isFork(pr)
}
