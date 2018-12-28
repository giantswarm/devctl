package githubclient

import (
	"strings"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/github"
)

type Repository struct {
	Name      string
	Language  string
	Owner     string
	UpdatedAt time.Time
}

func newRepository(ghRepo *github.Repository, owner string) (Repository, error) {
	if ghRepo == nil {
		return Repository{}, microerror.Maskf(executionError, "expected non nil argument but got %#v", ghRepo)
	}

	name, err := toString(ghRepo.Name)
	if err != nil {
		return Repository{}, microerror.Maskf(executionError, "expected non nil %T.Name value but got %#v", ghRepo, ghRepo.Name)
	}
	language, err := toString(ghRepo.Language)
	if err != nil {
		language = ""
	}
	updatedAt, err := toTime(ghRepo.UpdatedAt)
	if err != nil {
		return Repository{}, microerror.Maskf(executionError, "expected non nil %T.UpdatedAt value for repository %#q but got %#v", ghRepo, name, ghRepo.UpdatedAt)
	}

	r := Repository{
		Name:      name,
		Language:  language,
		Owner:     owner,
		UpdatedAt: updatedAt,
	}

	return r, nil
}

type RepositoryFile struct {
	Data []byte
	Path string
}

func newRepositoryFile(ghContent *github.RepositoryContent) (RepositoryFile, error) {
	if ghContent == nil {
		return RepositoryFile{}, microerror.Maskf(executionError, "expected non nil argument but got %#v", ghContent)
	}

	// Validate type.
	{
		typ, err := toString(ghContent.Type)
		if err != nil {
			return RepositoryFile{}, microerror.Mask(err)
		}

		if strings.ToLower(typ) != "file" {
		}
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

func toTime(p *github.Timestamp) (time.Time, error) {
	if p == nil {
		return time.Time{}, microerror.Maskf(executionError, "value is nil")
	}

	t := *p

	return t.Time, nil
}
