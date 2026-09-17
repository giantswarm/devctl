package reposetup

import (
	"bytes"
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v92/github"
)

// TeamFilesDir is the directory of giantswarm/github that holds the team
// files.
const TeamFilesDir = "repositories"

// Remote is the repository that holds the team files -- giantswarm/github at
// main -- read and written through GitHub as the caller: the person's token
// on a laptop, the App's in a workflow. What the token cannot see (a team
// membership, a private repository) is an error the caller reports; nothing
// is guessed.
type Remote struct {
	// GitHub is the client; required.
	GitHub *github.Client
	// Owner, Repo and Ref locate the team files; empty means
	// giantswarm/github at main.
	Owner, Repo, Ref string
}

// RemoteTeamFile is one team file as it stands on the remote, with the blob
// SHA a commit on top of it needs.
type RemoteTeamFile struct {
	*TeamFile
	// SHA is the blob's SHA at Ref.
	SHA string
	// Content is the file's text at Ref.
	Content []byte
}

// PullRequest is the change [Remote.OpenPullRequest] opens: one file
// written on a new branch off Ref and proposed to Ref.
type PullRequest struct {
	// Branch is the head branch to create; it must not exist.
	Branch string
	// Path and Content are the file and its new text; SHA is the blob SHA
	// the file has at Ref, so the write is refused when it moved.
	Path    string
	Content []byte
	SHA     string
	// CommitMessage, Title and Body are the commit's and the pull
	// request's.
	CommitMessage, Title, Body string
}

func (r Remote) owner() string {
	if r.Owner != "" {
		return r.Owner
	}
	return SchemaRepositoryOwner
}

func (r Remote) repo() string {
	if r.Repo != "" {
		return r.Repo
	}
	return SchemaRepository
}

func (r Remote) ref() string {
	if r.Ref != "" {
		return r.Ref
	}
	return SchemaRef
}

// Slug is owner/repo@ref, for messages.
func (r Remote) Slug() string {
	return fmt.Sprintf("%s/%s@%s", r.owner(), r.repo(), r.ref())
}

// TeamFilePath is the path of a team's file: repositories/<team>.yaml.
func TeamFilePath(team string) string {
	return path.Join(TeamFilesDir, team+".yaml")
}

// Teams lists the teams that have a team file, from the directory listing.
func (r Remote) Teams(ctx context.Context) ([]string, error) {
	if r.GitHub == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.GitHub must not be nil", r)
	}
	_, dir, _, err := r.GitHub.Repositories.GetContents(ctx, r.owner(), r.repo(), TeamFilesDir, &github.RepositoryContentGetOptions{Ref: r.ref()})
	if err != nil {
		return nil, microerror.Mask(err)
	}
	var teams []string
	for _, f := range dir {
		if f.GetType() == "file" && strings.HasSuffix(f.GetName(), ".yaml") {
			teams = append(teams, TeamOf(f.GetName()))
		}
	}
	return teams, nil
}

// TeamFile reads a team's file. A team without a file is
// [IsEntryNotFound]: the team is not one the team files know.
func (r Remote) TeamFile(ctx context.Context, team string) (*RemoteTeamFile, error) {
	if r.GitHub == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.GitHub must not be nil", r)
	}
	file, _, resp, err := r.GitHub.Repositories.GetContents(ctx, r.owner(), r.repo(), TeamFilePath(team), &github.RepositoryContentGetOptions{Ref: r.ref()})
	if err != nil {
		if resp != nil && resp.StatusCode == 404 {
			return nil, microerror.Maskf(entryNotFoundError, "no team file %s in %s: %q is not a team the team files know", TeamFilePath(team), r.Slug(), team)
		}
		return nil, microerror.Mask(err)
	}
	if file == nil {
		return nil, microerror.Maskf(invalidTeamFileError, "%s in %s is not a file", TeamFilePath(team), r.Slug())
	}
	content, err := file.GetContent()
	if err != nil {
		return nil, microerror.Mask(err)
	}
	tf, err := ParseTeamFile(team, strings.NewReader(content))
	if err != nil {
		return nil, microerror.Mask(err)
	}
	tf.Path = TeamFilePath(team)

	return &RemoteTeamFile{TeamFile: tf, SHA: file.GetSHA(), Content: []byte(content)}, nil
}

// FindEntry returns the team file that declares name, searching the given
// teams (every team with a file when nil). A name no file declares is
// [IsEntryNotFound].
func (r Remote) FindEntry(ctx context.Context, name string, teams []string) (*RemoteTeamFile, error) {
	if teams == nil {
		var err error
		teams, err = r.Teams(ctx)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}
	for _, team := range teams {
		tf, err := r.TeamFile(ctx, team)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		if _, ok := tf.Entry(name); ok {
			return tf, nil
		}
	}
	return nil, microerror.Maskf(entryNotFoundError, "no team file in %s declares %q (searched %s)", r.Slug(), name, strings.Join(teams, ", "))
}

// OpenPullRequest creates the branch off Ref, commits the file on it and
// opens the pull request against Ref, all as the caller.
func (r Remote) OpenPullRequest(ctx context.Context, pr PullRequest) (*github.PullRequest, error) {
	if r.GitHub == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.GitHub must not be nil", r)
	}
	if pr.Branch == "" || pr.Path == "" || pr.Title == "" {
		return nil, microerror.Maskf(invalidConfigError, "%T.Branch, Path and Title must not be empty", pr)
	}
	owner, repo := r.owner(), r.repo()

	base, _, err := r.GitHub.Git.GetRef(ctx, owner, repo, "heads/"+r.ref())
	if err != nil {
		return nil, microerror.Mask(err)
	}
	if _, resp, err := r.GitHub.Git.CreateRef(ctx, owner, repo, github.CreateRef{Ref: "refs/heads/" + pr.Branch, SHA: base.GetObject().GetSHA()}); err != nil {
		if resp != nil && resp.StatusCode == 422 {
			return nil, microerror.Maskf(branchExistsError, "branch %s exists in %s/%s: a pull request for this change may be open already", pr.Branch, owner, repo)
		}
		return nil, microerror.Mask(err)
	}

	message := pr.CommitMessage
	if message == "" {
		message = pr.Title
	}
	opts := &github.RepositoryContentFileOptions{
		Message: &message,
		Content: pr.Content,
		Branch:  &pr.Branch,
	}
	if pr.SHA != "" {
		opts.SHA = &pr.SHA
	}
	if _, _, err := r.GitHub.Repositories.UpdateFile(ctx, owner, repo, pr.Path, opts); err != nil {
		return nil, microerror.Mask(err)
	}

	created, _, err := r.GitHub.PullRequests.Create(ctx, owner, repo, github.CreatePullRequest{
		Title: &pr.Title,
		Head:  pr.Branch,
		Base:  r.ref(),
		Body:  &pr.Body,
	})
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return created, nil
}

// FindPullRequest returns the open pull request whose head is branch, or
// nil when none is open: what a caller looks for when the branch of the
// change it wants to open exists already.
func (r Remote) FindPullRequest(ctx context.Context, branch string) (*github.PullRequest, error) {
	if r.GitHub == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.GitHub must not be nil", r)
	}
	owner, repo := r.owner(), r.repo()
	prs, _, err := r.GitHub.PullRequests.List(ctx, owner, repo, &github.PullRequestListOptions{
		State:       "open",
		Head:        owner + ":" + branch,
		Base:        r.ref(),
		ListOptions: github.ListOptions{PerPage: 1},
	})
	if err != nil {
		return nil, microerror.Mask(err)
	}
	if len(prs) == 0 {
		return nil, nil
	}
	return prs[0], nil
}

// Person is the caller as GitHub knows them: the login and the slugs of
// the owner organisation's teams they belong to -- the inputs of the team
// guard. Teams need the token to read the organisation (read:org); a token
// without it gets the login and an error the caller turns into a warning.
type Person struct {
	Login string
	Teams []string
}

// Person reads the caller's login and team memberships in owner.
func (r Remote) Person(ctx context.Context, owner string) (Person, error) {
	if r.GitHub == nil {
		return Person{}, microerror.Maskf(invalidConfigError, "%T.GitHub must not be nil", r)
	}
	user, _, err := r.GitHub.Users.Get(ctx, "")
	if err != nil {
		return Person{}, microerror.Mask(err)
	}
	p := Person{Login: user.GetLogin()}

	opts := &github.ListOptions{PerPage: 100}
	for {
		teams, resp, err := r.GitHub.Teams.ListUserTeams(ctx, opts)
		if err != nil {
			return p, microerror.Mask(err)
		}
		for _, t := range teams {
			if strings.EqualFold(t.GetOrganization().GetLogin(), owner) {
				p.Teams = append(p.Teams, t.GetSlug())
			}
		}
		if resp.NextPage == 0 {
			return p, nil
		}
		opts.Page = resp.NextPage
	}
}

// CreatedRepository is the repository `devctl repo create` created as the
// person before opening the declaration's pull request: its URL and the
// commit holding its scaffold.
type CreatedRepository struct {
	URL            string
	ScaffoldCommit string
}

// CreationPullRequest is the pull request `devctl repo create` opens for an
// accepted declaration of a repository the person has just created: the
// branch, the conventional-commit title the semantic-pull-request check of
// giantswarm/github accepts, and a body that names the repository and its
// scaffold commit, the declaration, the template, the name check and the
// guard notices.
func CreationPullRequest(tf *RemoteTeamFile, content []byte, result *Result, created CreatedRepository) PullRequest {
	entry := result.Entries[0]
	var body bytes.Buffer
	fmt.Fprintf(&body, "Declares the repository `%s/%s` in `%s`, opened by `devctl repo create`.\n\n", DefaultOwner, entry.Name, tf.Path)
	if created.URL != "" {
		fmt.Fprintf(&body, "The repository exists, created by the author: %s", created.URL)
		if created.ScaffoldCommit != "" {
			fmt.Fprintf(&body, " (scaffold commit `%s`)", created.ScaffoldCommit)
		}
		body.WriteString(".\n\n")
	}
	fmt.Fprintf(&body, "```yaml\n%s```\n\n", entry.Rendered)
	if entry.Template != "" {
		fmt.Fprintf(&body, "Template: `%s`\n", entry.Template)
	}
	fmt.Fprintf(&body, "Name check: %s", entry.NameCheck.Verdict)
	if entry.NameCheck.Detail != "" {
		fmt.Fprintf(&body, " -- %s", entry.NameCheck.Detail)
	}
	body.WriteString("\n")
	if len(result.Notices) > 0 {
		body.WriteString("\nNotices:\n")
		for _, n := range result.Notices {
			fmt.Fprintf(&body, "- %s: %s\n", n.Kind, n.Message)
		}
	}
	body.WriteString("\nThe validation check classifies this change; a creation-only change is approved by the machine and the reconciler sets the repository up after the merge.\n")

	title := fmt.Sprintf("feat(%s): declare %s", strings.TrimPrefix(tf.Team, "team-"), entry.Name)
	return PullRequest{
		Branch:  "repo-create/" + entry.Name,
		Path:    tf.Path,
		Content: content,
		SHA:     tf.SHA,
		Title:   title,
		Body:    body.String(),
	}
}
