package prwait

import "github.com/google/go-github/v93/github"

// NotApplicable is the gate for pr merge before and after its wait: exit 3
// for a draft, closed, merged, conflicting or behind pull request, nil
// otherwise. Wait applies the same gate but allows drafts to wait for CI.
func NotApplicable(pr *github.PullRequest) error {
	return notApplicable(pr, false)
}

// IsFork says whether the head lives in another repository than the base.
func IsFork(pr *github.PullRequest) bool {
	return isFork(pr)
}
