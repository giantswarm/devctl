package file

import (
	"context"
	"fmt"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

// ChangelogPath is the hand-kept changelog a repository may carry.
const ChangelogPath = "CHANGELOG.md"

// NewChangelogPointerInput freezes CHANGELOG.md of a repository on the
// auto-release flow as a pointer at its GitHub Releases, which are the
// changelog there. Nothing writes the file afterwards, so no pull request
// edits it and none conflicts on it. Its earlier entries stay in the git
// history and in the CHANGELOG.md of every earlier tag.
func NewChangelogPointerInput(p params.Params) input.Input {
	return input.Input{
		Path:           ChangelogPath,
		SkipRegenCheck: true,
		Generate: func(_ context.Context) ([]byte, error) {
			return []byte(ChangelogPointer(p.RepoName)), nil
		},
	}
}

// ChangelogPointer is the content of a frozen CHANGELOG.md.
func ChangelogPointer(repoName string) string {
	return fmt.Sprintf(`# Changelog

The changes of each release are in its notes on GitHub Releases:
https://github.com/giantswarm/%s/releases

This file is not updated any more: a pull request does not edit it. The
pull request title is the release note.
`, repoName)
}
