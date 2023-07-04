package githubclient

import (
	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v44/github"
)

type RepositoryFile struct {
	Data []byte
	Path string
}

func newRepositoryFile(ghContent *github.RepositoryContent) (RepositoryFile, error) {
	if ghContent == nil {
		return RepositoryFile{}, microerror.Maskf(executionError, "expected non nil argument but got %#v", ghContent)
	}

	path, err := toString(ghContent.Path)
	if err != nil {
		return RepositoryFile{}, microerror.Mask(err)
	}

	data, err := ghContent.GetContent()
	if err != nil {
		return RepositoryFile{}, microerror.Mask(err)
	}

	r := RepositoryFile{
		Data: []byte(data),
		Path: path,
	}

	return r, err
}

func toString(p *string) (string, error) {
	if p == nil {
		return "", microerror.Maskf(executionError, "value is nil")
	}

	return *p, nil
}
