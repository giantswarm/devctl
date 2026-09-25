package reposetup

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v92/github"
)

// OverridesDir is the directory of giantswarm/github, beside the team files,
// that holds the align-files overrides: one directory per repository, whose
// files align-files writes into the repository in place of the generated
// ones.
const OverridesDir = TeamFilesDir + "/override"

// codeownersFile is the name of the CODEOWNERS file, in a repository and in
// its override directory.
const codeownersFile = "CODEOWNERS"

// CodeownersOverridePath is the path of a repository's CODEOWNERS override
// in giantswarm/github: repositories/override/<repository>/CODEOWNERS.
func CodeownersOverridePath(repository string) string {
	return path.Join(OverridesDir, repository, codeownersFile)
}

// ReadCodeownersOverride reads a repository's CODEOWNERS override from the
// checkout of giantswarm/github the team file at teamFilePath lives in:
// override/<repository>/CODEOWNERS beside the team file. It returns nil when
// the repository has none: align-files writes the generated file then.
func ReadCodeownersOverride(teamFilePath, repository string) ([]byte, error) {
	p := filepath.Join(filepath.Dir(teamFilePath), path.Base(OverridesDir), repository, codeownersFile)
	content, err := os.ReadFile(filepath.Clean(p))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, microerror.Mask(err)
	}
	return content, nil
}

// Overrides are the repositories that have an override directory in
// giantswarm/github at the [Remote]'s ref, listed once: a caller that
// resolves many entries holds one Overrides and asks it per repository, and
// only a repository the listing names costs a request. It is safe for
// concurrent use.
type Overrides struct {
	remote Remote
	names  map[string]bool
}

// Overrides lists the override directory at Ref, one request. A ref without
// the directory has no overrides.
func (r Remote) Overrides(ctx context.Context) (*Overrides, error) {
	if r.GitHub == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.GitHub must not be nil", r)
	}
	_, dir, resp, err := r.GitHub.Repositories.GetContents(ctx, r.owner(), r.repo(), OverridesDir, &github.RepositoryContentGetOptions{Ref: r.ref()})
	o := &Overrides{remote: r, names: map[string]bool{}}
	switch {
	case resp != nil && resp.StatusCode == 404:
		return o, nil
	case err != nil:
		return nil, microerror.Mask(err)
	}
	for _, f := range dir {
		if f.GetType() == "dir" {
			o.names[f.GetName()] = true
		}
	}
	return o, nil
}

// Codeowners reads a repository's CODEOWNERS override at the listing's ref.
// It returns nil when the repository has none -- no override directory (no
// request), or one without a CODEOWNERS -- and align-files writes the
// generated file then.
func (o *Overrides) Codeowners(ctx context.Context, repository string) ([]byte, error) {
	if !o.names[repository] {
		return nil, nil
	}
	r := o.remote
	file, _, resp, err := r.GitHub.Repositories.GetContents(ctx, r.owner(), r.repo(), CodeownersOverridePath(repository), &github.RepositoryContentGetOptions{Ref: r.ref()})
	switch {
	case resp != nil && resp.StatusCode == 404:
		return nil, nil
	case err != nil:
		return nil, microerror.Mask(err)
	case file == nil:
		return nil, microerror.Maskf(invalidConfigError, "%s in %s is not a file", CodeownersOverridePath(repository), r.Slug())
	}
	content, err := file.GetContent()
	if err != nil {
		return nil, microerror.Mask(err)
	}
	return []byte(content), nil
}
