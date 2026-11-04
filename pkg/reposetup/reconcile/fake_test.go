package reconcile

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

// The fakes below stand in for GitHub's and CircleCI's REST surfaces: an
// in-memory state behind the endpoints the steps use, seeded by a test and
// inspected afterwards. Every non-GET request is recorded as a mutation so a
// test can assert that a converged run changes nothing.

// fakeRepo is one repository's state.
type fakeRepo struct {
	owner, name, description string
	private, archived        bool
	topics                   []string
	hasWiki, hasIssues, hasProjects, allowMerge, allowSquash, allowRebase,
	allowUpdate, allowAuto, deleteOnMerge bool
	squashTitle   string // PR_TITLE or COMMIT_OR_PR_TITLE
	defaultBranch string
	workflowPerm  string
	teams         map[string]string
	collaborators map[string]string // login → permission granted directly
	empty         bool
	files         map[string]string            // default-branch files
	branchFiles   map[string]map[string]string // other branches
	protection    *fakeProtection
	protected     string // the branch the last protection PUT named
	rulesets      []*github.RepositoryRuleset
	hooks         []*github.Hook
	hooksStatus   int // HTTP status of the hooks list when not 200
	deployKeys    []*github.Key
	keysStatus    int // HTTP status of the deploy keys list when not 200
	release       string
	releaseAt     time.Time
	// releases is the release list, newest first, as GitHub lists it;
	// releasesPage pages it when set.
	releases     []fakeRelease
	releasesPage int
	createdAt    time.Time // when the repository was created; a month ago for a seeded one
	statuses     []string  // commit statuses reported on the head
	// refStatuses are the statuses of a ref's commit, a branch or a tag,
	// newest first: what GET /commits/{ref}/statuses lists.
	refStatuses  map[string][]fakeStatus
	checkRuns    []string // check runs reported on the head
	checksStatus int      // HTTP status of the status and check-run reads when not 200
	prs          []*github.PullRequest
	// merged are the pull requests merged before the run, closed, as the
	// discovery of the reported checks lists them; the statuses and check
	// runs are served for their heads as for any ref.
	merged []*github.PullRequest
	// pullsStatus, when set, answers the pull request list with it (403 for
	// an identity without pull-request read).
	pullsStatus int
	// refStatusesStatus answers the statuses of a ref with the code set for
	// it (403 for an identity without statuses read on that commit).
	refStatusesStatus map[string]int
	issues            []*github.Issue            // what GET /repos/{owner}/{repo}/issues lists
	labels            map[string]*github.Label   // by lower-cased name: GitHub compares label names case-insensitively
	history           []*github.RepositoryCommit // the commits behind the head of the default branch
	issuesStatus      int                        // HTTP status of the issues list when not 200
	blobs             map[string][]byte
	trees             map[string][]*github.TreeEntry
	commits           map[string]fakeCommit // commit sha → tree sha and message
	heads             map[string]string     // branch → sha of its head commit
	seq               int

	// protectionStatus is the HTTP status of the classic protection read
	// when not 200 (403 for an integration without administration: read).
	protectionStatus int

	// releaseSubject is the subject of the commit the release's tag names;
	// empty leaves the tag's commit unknown (404).
	releaseSubject string
}

type fakeCommit struct{ tree, message string }

// headSubject is the subject of the commit at the head of branch, what the
// auto-release workflow decides the version bump from.
func (r *fakeRepo) headSubject(branch string) string {
	subject, _, _ := strings.Cut(r.commits[r.heads[branch]].message, "\n")
	return subject
}

type fakeProtection struct {
	reviews                             int
	enforceAdmins, allowForce, allowDel bool
	strict                              bool
	checks                              []string
}

// ruleset returns the repository's ruleset named name, nil without one.
func (r *fakeRepo) ruleset(name string) *github.RepositoryRuleset {
	for _, rs := range r.rulesets {
		if rs.Name == name {
			return rs
		}
	}
	return nil
}

// addRuleset seeds a ruleset of the shape the engine writes for an aligned
// repository — active on ~DEFAULT_BRANCH, one review, deletion and force
// pushes forbidden, the checks given, the bypass actors given — for a test
// to bend into a drift case.
func (r *fakeRepo) addRuleset(name string, checks []*github.RuleStatusCheck, bypass ...*github.BypassActor) *github.RepositoryRuleset {
	rules := &github.RepositoryRulesetRules{
		PullRequest:    &github.PullRequestRuleParameters{RequiredApprovingReviewCount: 1},
		Deletion:       &github.EmptyRuleParameters{},
		NonFastForward: &github.EmptyRuleParameters{},
	}
	if len(checks) > 0 {
		rules.RequiredStatusChecks = &github.RequiredStatusChecksRuleParameters{RequiredStatusChecks: checks}
	}
	r.seq++
	rs := &github.RepositoryRuleset{
		ID: new(int64(r.seq)), Name: name, Target: new(github.RulesetTargetBranch), Enforcement: github.RulesetEnforcementActive,
		BypassActors: bypass,
		Conditions:   &github.RepositoryRulesetConditions{RefName: &github.RepositoryRulesetRefConditionParameters{Include: []string{"~DEFAULT_BRANCH"}, Exclude: []string{}}},
		Rules:        rules,
	}
	r.rulesets = append(r.rulesets, rs)
	return rs
}

// statusCheck is a required check any integration satisfies (a CircleCI
// status); actionsCheck one pinned to the GitHub Actions App.
func statusCheck(context string) *github.RuleStatusCheck {
	return &github.RuleStatusCheck{Context: context}
}

func actionsCheck(context string) *github.RuleStatusCheck {
	return &github.RuleStatusCheck{Context: context, IntegrationID: new(int64(15368))}
}

// appBypass is a GitHub App as bypass actor for pull requests.
func appBypass(id int64) *github.BypassActor {
	return &github.BypassActor{ActorID: new(id), ActorType: new(github.BypassActorTypeIntegration), BypassMode: new(github.BypassModePullRequest)}
}

// adminBypass is the repository role Admin as bypass actor for pull requests.
func adminBypass() *github.BypassActor {
	return &github.BypassActor{ActorID: new(int64(5)), ActorType: new(github.BypassActorTypeRepositoryRole), BypassMode: new(github.BypassModePullRequest)}
}

// teamBypass is a team as bypass actor for pull requests.
func teamBypass(id int64) *github.BypassActor {
	return &github.BypassActor{ActorID: new(id), ActorType: new(github.BypassActorTypeTeam), BypassMode: new(github.BypassModePullRequest)}
}

// testTeamID is the id of the owning team in the fake organization.
const testTeamID int64 = 7

// checkContexts lists the contexts of a ruleset's required_status_checks
// rule, nil without the rule.
func checkContexts(rs *github.RepositoryRuleset) []string {
	if rs == nil || rs.Rules == nil || rs.Rules.RequiredStatusChecks == nil {
		return nil
	}
	var names []string
	for _, c := range rs.Rules.RequiredStatusChecks.RequiredStatusChecks {
		names = append(names, c.Context)
	}
	return names
}

// fakeGitHub is the GitHub fake.
type fakeGitHub struct {
	mu        sync.Mutex
	repos     map[string]*fakeRepo // owner/name
	redirects map[string]string    // owner/old → owner/new
	// installation is GET /user/installations/{id}/repositories.
	installation struct {
		status    int
		selection string
		repos     []string
	}
	runs       map[string][]string // owner/repo/workflow → run statuses
	dispatches []string
	// permission is what any user holds on any repository through the
	// organization's teams unless a direct grant says otherwise.
	permission string
	// orgTeams are the organization's teams by slug (GET
	// /orgs/{org}/teams/{slug}); the owning team is seeded visible
	// (privacy closed). A ruleset write refuses a Team bypass actor that is
	// secret or not one of them with 422, as GitHub does.
	orgTeams map[string]*github.Team
	// teamBypassRefused, when set, has every ruleset write refuse a Team
	// bypass actor with 422 whatever the team's privacy: GitHub's judgment
	// beyond what the team read shows.
	teamBypassRefused bool
	// readIdentity, when set, serves a ruleset without its bypass_actors,
	// as GitHub does to an identity without write access to the ruleset.
	readIdentity bool
	// orgRole is the caller's role in any organization (GET
	// /user/memberships/orgs/{org}); "" answers 404, not a member.
	orgRole string
	// createStatus, when not 0, is the status POST /orgs/{org}/repos answers
	// instead of creating — 403 for a member of an organization that does
	// not let members create repositories.
	createStatus int
	// onDispatch simulates what a dispatched workflow lands.
	onDispatch func(workflow string, inputs map[string]any)
	// runToken, when set, is a workflow run's own token beside the fake's
	// default identity (no bearer: the App or a person). It sees public
	// repositories only — a private one answers 404 to it on every
	// endpoint — and is the one identity that may list and dispatch
	// workflow runs: the default identity gets 403 there, GitHub's answer
	// to an App without an Actions permission. Empty models one identity
	// that may do everything.
	runToken string
	// dispatchedBy records the bearer token of every dispatch, "" for none.
	dispatchedBy []string
	// readOnly says the fake's default identity holds no admin on any
	// repository: GET /repos/{owner}/{repo} then omits the six merge
	// settings, as GitHub does for such an identity; GraphQL carries them
	// to it as to any identity that reads the repository.
	readOnly bool
	// graphqlStatus, when not 0, is the status POST /graphql answers instead
	// of the query.
	graphqlStatus int
	mutations     []string
	// gets records the path of every GET: what a run costs in requests.
	gets []string
	srv  *httptest.Server
}

// reads counts the GETs of path so far.
func (f *fakeGitHub) reads(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, p := range f.gets {
		if p == path {
			n++
		}
	}
	return n
}

// bearer is the request's bearer token, "" without one.
func bearer(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}

// isRunToken says whether r runs under the workflow run's token.
func (f *fakeGitHub) isRunToken(r *http.Request) bool {
	return f.runToken != "" && bearer(r) == f.runToken
}

// mayDispatch says whether r's identity holds the Actions permission: any
// without a run token, the run token alone with one.
func (f *fakeGitHub) mayDispatch(r *http.Request) bool {
	return f.runToken == "" || f.isRunToken(r)
}

func newFakeGitHub() *fakeGitHub {
	f := &fakeGitHub{repos: map[string]*fakeRepo{}, redirects: map[string]string{}, runs: map[string][]string{}, permission: "admin", orgRole: "admin"}
	f.orgTeams = map[string]*github.Team{team: {ID: new(testTeamID), Slug: new(team), Name: new("Team Bumblebee"), Privacy: new("closed")}}
	f.installation.status = http.StatusOK
	f.installation.selection = "selected"
	mux := http.NewServeMux()
	f.routes(mux)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = strings.TrimPrefix(r.URL.Path, "/api/v3") // go-github's enterprise prefix
		// The escaped path too: a label named team/<team> travels as
		// team%2F<team>, one path segment.
		r.URL.RawPath = strings.TrimPrefix(r.URL.RawPath, "/api/v3")
		f.mu.Lock()
		if r.Method != http.MethodGet && r.URL.Path != "/graphql" {
			f.mutations = append(f.mutations, r.Method+" "+r.URL.Path)
		} else {
			f.gets = append(f.gets, r.URL.Path) // a GraphQL query is a read
		}
		f.mu.Unlock()
		mux.ServeHTTP(w, r)
	}))
	return f
}

// addRepo seeds a repository at the baseline with a scaffold, active and
// unprotected; the test adjusts it.
// forkLine shapes the repository as a fork line has it on GitHub: on the
// branch named after the organisation, its pull requests landing by rebase
// merge beside squash.
func (r *fakeRepo) forkLine() *fakeRepo {
	r.defaultBranch, r.allowRebase = "giantswarm", true
	return r
}

// hasBranch says whether the repository carries the branch: its default, one
// with files of its own or one with a head commit.
func (r *fakeRepo) hasBranch(branch string) bool {
	_, files := r.branchFiles[branch]
	_, head := r.heads[branch]
	return branch == r.defaultBranch || files || head
}

func (f *fakeGitHub) addRepo(owner, name string) *fakeRepo {
	r := &fakeRepo{
		owner: owner, name: name,
		hasIssues: true, allowSquash: true, allowUpdate: true, allowAuto: true, deleteOnMerge: true,
		squashTitle:   "PR_TITLE",
		defaultBranch: "main", workflowPerm: "write",
		createdAt:     time.Now().Add(-30 * 24 * time.Hour),
		teams:         map[string]string{"employees": "admin", "bots": "push"},
		collaborators: map[string]string{},
		files:         map[string]string{},
		branchFiles:   map[string]map[string]string{},
		blobs:         map[string][]byte{}, trees: map[string][]*github.TreeEntry{}, commits: map[string]fakeCommit{},
		heads:  map[string]string{},
		labels: map[string]*github.Label{TeamLabel(team): {Name: TeamLabel(team), Color: DefaultLabelColor}},
		merged: []*github.PullRequest{{
			Number: new(1), State: new("closed"), MergedAt: &github.Timestamp{Time: time.Now().Add(-24 * time.Hour)},
			Head: &github.PullRequestBranch{SHA: new("merged-head"), Ref: new("merged")}, Base: &github.PullRequestBranch{Ref: new("main")},
		}},
	}
	for p, c := range scaffoldFiles {
		r.files[p] = c
	}
	f.repos[owner+"/"+name] = r
	return r
}

// refusesBypass answers 422 to a ruleset write naming a Team bypass actor
// GitHub would not take — a secret team, a team not of the organization,
// or any team with teamBypassRefused — and says whether it did.
func (f *fakeGitHub) refusesBypass(w http.ResponseWriter, actors []*github.BypassActor) bool {
	for _, a := range actors {
		if a.ActorType == nil || *a.ActorType != github.BypassActorTypeTeam {
			continue
		}
		var known *github.Team
		for _, t := range f.orgTeams {
			if t.GetID() == a.GetActorID() {
				known = t
			}
		}
		if f.teamBypassRefused || known == nil || known.GetPrivacy() == "secret" {
			writeJSON(w, 422, map[string]any{"message": "Validation Failed", "errors": []map[string]string{{"resource": "Ruleset", "field": "bypass_actors", "code": "custom", "message": "Bypass actors must be part of the ruleset source or owner organization"}}})
			return true
		}
	}
	return false
}

// rulesetIndex is the position of the ruleset with the id in the path, -1
// without one.
func (r *fakeRepo) rulesetIndex(id string) int {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return -1
	}
	for i, rs := range r.rulesets {
		if rs.GetID() == n {
			return i
		}
	}
	return -1
}

func (f *fakeGitHub) repo(owner, name string) (*fakeRepo, bool) {
	slug := owner + "/" + name
	if target, ok := f.redirects[strings.ToLower(slug)]; ok {
		slug = target
	}
	r, ok := f.repos[strings.ToLower(slug)]
	if !ok {
		r, ok = f.repos[slug]
	}
	return r, ok
}

func (r *fakeRepo) next(prefix string) string {
	r.seq++
	return fmt.Sprintf("%s%04d", prefix, r.seq)
}

// addIssue seeds an open issue created by login — or a pull request, as the
// issues endpoint lists pull requests too.
func (r *fakeRepo) addIssue(login, title string, pullRequest bool) *github.Issue {
	n := len(r.issues) + 1
	is := &github.Issue{Number: new(n), State: new("open"), Title: new(title), User: &github.User{Login: new(login)}}
	if pullRequest {
		is.PullRequestLinks = &github.PullRequestLinks{URL: new(fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d", r.owner, r.name, n))}
	}
	r.issues = append(r.issues, is)
	return is
}

func (r *fakeRepo) toGitHub() *github.Repository {
	return &github.Repository{
		Name:                   new(r.name),
		FullName:               new(r.owner + "/" + r.name),
		HTMLURL:                new("https://github.com/" + r.owner + "/" + r.name),
		Owner:                  &github.User{Login: new(r.owner)},
		Description:            new(r.description),
		Private:                new(r.private),
		Topics:                 r.topics,
		CreatedAt:              &github.Timestamp{Time: r.createdAt},
		Archived:               new(r.archived),
		HasWiki:                new(r.hasWiki),
		HasIssues:              new(r.hasIssues),
		HasProjects:            new(r.hasProjects),
		AllowMergeCommit:       new(r.allowMerge),
		AllowSquashMerge:       new(r.allowSquash),
		AllowRebaseMerge:       new(r.allowRebase),
		AllowUpdateBranch:      new(r.allowUpdate),
		AllowAutoMerge:         new(r.allowAuto),
		DeleteBranchOnMerge:    new(r.deleteOnMerge),
		SquashMergeCommitTitle: new(r.squashTitle),
		DefaultBranch:          new(r.defaultBranch),
	}
}

// rulesetJSON is the ruleset as GitHub serves it: bypass_actors always
// present for an identity with write access to the ruleset, an empty array
// when there are none, and left out for a read identity.
func rulesetJSON(rs *github.RepositoryRuleset, readIdentity bool) map[string]any {
	b, err := json.Marshal(rs)
	if err != nil {
		panic(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		panic(err)
	}
	if readIdentity {
		delete(m, "bypass_actors")
	} else if len(rs.BypassActors) == 0 {
		m["bypass_actors"] = []any{}
	}
	return m
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func notFound(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusNotFound, map[string]string{"message": msg})
}

// forbidden is GitHub's answer to an identity without the permission.
// archivedReadOnly is GitHub's answer to a write an archived repository
// does not take.
func archivedReadOnly(w http.ResponseWriter) {
	writeJSON(w, http.StatusForbidden, map[string]string{"message": "Repository was archived so is read-only."})
}

func forbidden(w http.ResponseWriter) {
	writeJSON(w, http.StatusForbidden, map[string]string{"message": "Resource not accessible by integration"})
}

func decode(r *http.Request, v any) {
	_ = json.NewDecoder(r.Body).Decode(v)
}

// fakeRelease is one release of a repository's release list.
type fakeRelease struct {
	tag               string
	at                time.Time
	draft, prerelease bool
}

func (f *fakeGitHub) withRepo(h func(w http.ResponseWriter, r *http.Request, repo *fakeRepo)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		repo, ok := f.repo(r.PathValue("owner"), r.PathValue("repo"))
		if !ok || (repo.private && f.isRunToken(r)) {
			notFound(w, "Not Found")
			return
		}
		h(w, r, repo)
	}
}

func (f *fakeGitHub) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /repos/{owner}/{repo}", f.withRepo(func(w http.ResponseWriter, _ *http.Request, repo *fakeRepo) {
		out := repo.toGitHub()
		if f.readOnly {
			out.AllowMergeCommit, out.AllowSquashMerge, out.AllowRebaseMerge = nil, nil, nil
			out.AllowUpdateBranch, out.AllowAutoMerge, out.DeleteBranchOnMerge = nil, nil, nil
			out.SquashMergeCommitTitle = nil
		}
		writeJSON(w, 200, out)
	}))
	mux.HandleFunc("POST /graphql", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.graphqlStatus != 0 {
			writeJSON(w, f.graphqlStatus, map[string]string{"message": "Resource not accessible by integration"})
			return
		}
		var in struct {
			Query     string            `json:"query"`
			Variables map[string]string `json:"variables"`
		}
		decode(r, &in)
		slug := in.Variables["owner"] + "/" + in.Variables["name"]
		repo, ok := f.repo(in.Variables["owner"], in.Variables["name"])
		if !ok || !strings.Contains(in.Query, "repository(") {
			writeJSON(w, 200, map[string]any{"data": map[string]any{"repository": nil},
				"errors": []map[string]string{{"message": "Could not resolve to a Repository with the name '" + slug + "'."}}})
			return
		}
		writeJSON(w, 200, map[string]any{"data": map[string]any{"repository": map[string]any{
			"mergeCommitAllowed": repo.allowMerge, "squashMergeAllowed": repo.allowSquash, "rebaseMergeAllowed": repo.allowRebase,
			"allowUpdateBranch": repo.allowUpdate, "autoMergeAllowed": repo.allowAuto, "deleteBranchOnMerge": repo.deleteOnMerge,
			"squashMergeCommitTitle": repo.squashTitle,
		}}})
	})
	mux.HandleFunc("GET /user/memberships/orgs/{org}", func(w http.ResponseWriter, r *http.Request) {
		if f.orgRole == "" {
			writeJSON(w, 404, map[string]string{"message": "Not Found"})
			return
		}
		writeJSON(w, 200, map[string]string{"state": "active", "role": f.orgRole, "organization_url": "https://api.github.com/orgs/" + r.PathValue("org")})
	})
	mux.HandleFunc("POST /orgs/{owner}/repos", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.createStatus != 0 {
			writeJSON(w, f.createStatus, map[string]string{"message": "Resource not accessible by personal access token"})
			return
		}
		var in github.Repository
		decode(r, &in)
		repo := f.addRepo(r.PathValue("owner"), in.GetName())
		repo.description, repo.private = in.GetDescription(), in.GetPrivate()
		repo.createdAt = time.Now()
		repo.hasWiki, repo.teams = true, map[string]string{} // GitHub's defaults, not the baseline
		repo.squashTitle = "COMMIT_OR_PR_TITLE"
		repo.files = map[string]string{}
		repo.labels = map[string]*github.Label{} // GitHub's defaults, no team's
		repo.empty = !in.GetAutoInit()
		if in.GetAutoInit() {
			repo.files["README.md"] = "# " + repo.name + "\n"
		}
		writeJSON(w, 201, repo.toGitHub())
	})
	mux.HandleFunc("PATCH /repos/{owner}/{repo}", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		var in map[string]any
		decode(r, &in)
		set := func(key string, dst *bool) {
			if v, ok := in[key].(bool); ok {
				*dst = v
			}
		}
		set("private", &repo.private)
		set("archived", &repo.archived)
		set("has_wiki", &repo.hasWiki)
		set("has_issues", &repo.hasIssues)
		set("has_projects", &repo.hasProjects)
		set("allow_merge_commit", &repo.allowMerge)
		set("allow_squash_merge", &repo.allowSquash)
		set("allow_rebase_merge", &repo.allowRebase)
		set("allow_update_branch", &repo.allowUpdate)
		set("allow_auto_merge", &repo.allowAuto)
		set("delete_branch_on_merge", &repo.deleteOnMerge)
		if v, ok := in["squash_merge_commit_title"].(string); ok {
			repo.squashTitle = v
		}
		if v, ok := in["description"].(string); ok {
			repo.description = v
		}
		if v, ok := in["default_branch"].(string); ok {
			if !repo.hasBranch(v) {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": "Validation Failed"})
				return
			}
			if _, ok := repo.heads[repo.defaultBranch]; !ok && v != repo.defaultBranch {
				repo.heads[repo.defaultBranch] = "head" // the old default stays a branch
			}
			repo.defaultBranch = v
		}
		writeJSON(w, 200, repo.toGitHub())
	}))
	mux.HandleFunc("PUT /repos/{owner}/{repo}/topics", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		var in struct {
			Names []string `json:"names"`
		}
		decode(r, &in)
		repo.topics = in.Names
		writeJSON(w, 200, in)
	}))
	mux.HandleFunc("DELETE /repos/{owner}/{repo}", f.withRepo(func(w http.ResponseWriter, _ *http.Request, repo *fakeRepo) {
		delete(f.repos, repo.owner+"/"+repo.name)
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("GET /repos/{owner}/{repo}/commits", f.withRepo(func(w http.ResponseWriter, _ *http.Request, repo *fakeRepo) {
		if repo.empty {
			writeJSON(w, http.StatusConflict, map[string]string{"message": "Git Repository is empty."})
			return
		}
		writeJSON(w, 200, append([]*github.RepositoryCommit{{SHA: new("head")}}, repo.history...))
	}))
	mux.HandleFunc("GET /repos/{owner}/{repo}/commits/{ref}", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		if ref := r.PathValue("ref"); ref != repo.release || repo.releaseSubject == "" {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "No commit found for SHA: " + ref})
			return
		}
		writeJSON(w, 200, &github.RepositoryCommit{SHA: new("release-commit"), Commit: &github.Commit{Message: new(repo.releaseSubject + "\n\nbody")}})
	}))
	mux.HandleFunc("GET /repos/{owner}/{repo}/commits/{sha}/status", f.withRepo(func(w http.ResponseWriter, _ *http.Request, repo *fakeRepo) {
		if repo.checksStatus != 0 {
			writeJSON(w, repo.checksStatus, map[string]string{"message": "Resource not accessible by integration"})
			return
		}
		statuses := []map[string]string{}
		for _, c := range repo.statuses {
			statuses = append(statuses, map[string]string{"context": c, "state": "success"})
		}
		writeJSON(w, 200, map[string]any{"state": "success", "statuses": statuses})
	}))
	// The statuses of a ref's commit. The ref may hold slashes (a
	// multi-image tag, go/v0.10.0), so the pattern takes the rest of the
	// path and the handler cuts the suffix.
	mux.HandleFunc("GET /repos/{owner}/{repo}/commits/{ref...}", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		ref, ok := strings.CutSuffix(r.PathValue("ref"), "/statuses")
		if !ok {
			notFound(w, "Not Found")
			return
		}
		if code := repo.refStatusesStatus[ref]; code != 0 {
			writeJSON(w, code, map[string]string{"message": "Resource not accessible by integration"})
			return
		}
		list := repo.refStatuses[ref]
		out := []map[string]any{}
		for _, st := range list {
			out = append(out, map[string]any{
				"context": st.context, "state": st.state, "target_url": st.targetURL,
				"created_at": st.at.Format(time.RFC3339), "creator": map[string]string{"login": st.creator},
			})
		}
		writeJSON(w, 200, out)
	}))
	mux.HandleFunc("GET /repos/{owner}/{repo}/commits/{sha}/check-runs", f.withRepo(func(w http.ResponseWriter, _ *http.Request, repo *fakeRepo) {
		runs := []map[string]string{}
		for _, c := range repo.checkRuns {
			runs = append(runs, map[string]string{"name": c, "status": "completed", "conclusion": "success"})
		}
		writeJSON(w, 200, map[string]any{"total_count": len(runs), "check_runs": runs})
	}))
	mux.HandleFunc("GET /repos/{owner}/{repo}/pulls", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		if repo.pullsStatus != 0 {
			writeJSON(w, repo.pullsStatus, map[string]string{"message": "Resource not accessible by integration"})
			return
		}
		var out []*github.PullRequest
		for _, pr := range append(append([]*github.PullRequest{}, repo.prs...), repo.merged...) {
			if s := r.URL.Query().Get("state"); s != "" && s != "all" && pr.GetState() != s {
				continue
			}
			if h := r.URL.Query().Get("head"); h != "" && repo.owner+":"+pr.GetHead().GetRef() != h {
				continue
			}
			if b := r.URL.Query().Get("base"); b != "" && pr.GetBase().GetRef() != "" && pr.GetBase().GetRef() != b {
				continue
			}
			out = append(out, pr)
		}
		if out == nil {
			out = []*github.PullRequest{}
		}
		writeJSON(w, 200, out)
	}))
	mux.HandleFunc("GET /repos/{owner}/{repo}/issues", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		if repo.issuesStatus != 0 {
			writeJSON(w, repo.issuesStatus, map[string]string{"message": "Resource not accessible by integration"})
			return
		}
		out := []*github.Issue{}
		for _, is := range repo.issues {
			if c := r.URL.Query().Get("creator"); c != "" && is.GetUser().GetLogin() != c {
				continue
			}
			if s := r.URL.Query().Get("state"); s != "" && s != "all" && is.GetState() != s {
				continue
			}
			out = append(out, is)
		}
		writeJSON(w, 200, out)
	}))
	mux.HandleFunc("GET /repos/{owner}/{repo}/labels/{name}", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		l, ok := repo.labels[strings.ToLower(r.PathValue("name"))]
		if !ok {
			notFound(w, "Not Found")
			return
		}
		writeJSON(w, 200, l)
	}))
	mux.HandleFunc("POST /repos/{owner}/{repo}/labels", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		var in github.CreateIssueLabelRequest
		decode(r, &in)
		key := strings.ToLower(in.Name)
		if _, ok := repo.labels[key]; ok {
			writeJSON(w, 422, map[string]any{"message": "Validation Failed", "errors": []map[string]string{{"resource": "Label", "field": "name", "code": "already_exists"}}})
			return
		}
		l := &github.Label{Name: in.Name, Color: in.GetColor(), Description: in.Description}
		repo.labels[key] = l
		writeJSON(w, 201, l)
	}))
	mux.HandleFunc("POST /repos/{owner}/{repo}/pulls", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		var in github.CreatePullRequest
		decode(r, &in)
		n := len(repo.prs) + 1
		pr := &github.PullRequest{
			Number:  new(n),
			State:   new("open"),
			Title:   in.Title,
			Body:    in.Body,
			HTMLURL: new(fmt.Sprintf("https://github.com/%s/%s/pull/%d", repo.owner, repo.name, n)),
			Head:    &github.PullRequestBranch{Ref: new(in.Head)},
			Base:    &github.PullRequestBranch{Ref: new(in.Base)},
		}
		repo.prs = append(repo.prs, pr)
		writeJSON(w, 201, pr)
	}))
	mux.HandleFunc("GET /repos/{owner}/{repo}/contents/{path...}", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		path := strings.TrimSuffix(r.PathValue("path"), "/")
		files := repo.files
		if ref := r.URL.Query().Get("ref"); ref != "" && ref != repo.defaultBranch {
			files = repo.branchFiles[ref]
		}
		if repo.empty {
			notFound(w, "This repository is empty.")
			return
		}
		if content, ok := files[path]; ok {
			writeJSON(w, 200, map[string]any{
				"type": "file", "name": filepath.Base(path), "path": path, "encoding": "base64",
				"content": base64.StdEncoding.EncodeToString([]byte(content)), "sha": "blob-" + path, "size": len(content),
			})
			return
		}
		var listing []map[string]string
		seen := map[string]bool{}
		for p := range files {
			if path != "" && !strings.HasPrefix(p, path+"/") {
				continue
			}
			rest := strings.TrimPrefix(p, path+"/")
			if path == "" {
				rest = p
			}
			top := strings.SplitN(rest, "/", 2)[0]
			if seen[top] {
				continue
			}
			seen[top] = true
			kind := "file"
			if strings.Contains(rest, "/") {
				kind = "dir"
			}
			listing = append(listing, map[string]string{"type": kind, "name": top, "path": strings.TrimPrefix(path+"/"+top, "/")})
		}
		if listing == nil {
			notFound(w, "Not Found")
			return
		}
		sort.Slice(listing, func(i, j int) bool { return listing[i]["name"] < listing[j]["name"] })
		writeJSON(w, 200, listing)
	}))
	mux.HandleFunc("PUT /repos/{owner}/{repo}/contents/{path...}", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		var in struct {
			Message string `json:"message"`
			Content string `json:"content"`
			Branch  string `json:"branch"`
		}
		decode(r, &in)
		data, _ := base64.StdEncoding.DecodeString(in.Content)
		path := r.PathValue("path")
		branch := in.Branch
		if branch == "" {
			branch = repo.defaultBranch
		}
		if branch == repo.defaultBranch {
			repo.files[path] = string(data)
			repo.empty = false
		} else {
			if repo.branchFiles[branch] == nil {
				repo.branchFiles[branch] = map[string]string{}
			}
			repo.branchFiles[branch][path] = string(data)
		}
		sha := repo.next("c")
		repo.commits[sha] = fakeCommit{message: in.Message}
		repo.heads[branch] = sha
		writeJSON(w, 201, map[string]any{"content": map[string]string{"path": path}, "commit": map[string]string{"sha": sha}})
	}))
	mux.HandleFunc("GET /repos/{owner}/{repo}/git/ref/{ref...}", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		writeJSON(w, 200, map[string]any{"ref": "refs/" + r.PathValue("ref"), "object": map[string]string{"sha": "head", "type": "commit"}})
	}))
	mux.HandleFunc("POST /repos/{owner}/{repo}/git/refs", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		var in github.CreateRef
		decode(r, &in)
		branch := strings.TrimPrefix(in.Ref, "refs/heads/")
		repo.branchFiles[branch] = map[string]string{}
		for p, c := range repo.files {
			repo.branchFiles[branch][p] = c
		}
		writeJSON(w, 201, map[string]any{"ref": in.Ref, "object": map[string]string{"sha": in.SHA}})
	}))
	mux.HandleFunc("PATCH /repos/{owner}/{repo}/git/refs/{ref...}", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		var in github.UpdateRef
		decode(r, &in)
		commit, ok := repo.commits[in.SHA]
		if !ok {
			writeJSON(w, 422, map[string]string{"message": "unknown commit " + in.SHA})
			return
		}
		files := map[string]string{}
		for _, e := range repo.trees[commit.tree] {
			switch {
			case e.Content != nil:
				files[e.GetPath()] = e.GetContent()
			case e.SHA != nil:
				files[e.GetPath()] = string(repo.blobs[e.GetSHA()])
			}
		}
		repo.files = files
		repo.empty = false
		repo.heads[strings.TrimPrefix(r.PathValue("ref"), "heads/")] = in.SHA
		writeJSON(w, 200, map[string]any{"ref": "refs/" + r.PathValue("ref"), "object": map[string]string{"sha": in.SHA}})
	}))
	mux.HandleFunc("POST /repos/{owner}/{repo}/git/blobs", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		var in github.Blob
		decode(r, &in)
		data, _ := base64.StdEncoding.DecodeString(in.GetContent())
		sha := repo.next("b")
		repo.blobs[sha] = data
		writeJSON(w, 201, map[string]string{"sha": sha})
	}))
	mux.HandleFunc("POST /repos/{owner}/{repo}/git/trees", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		if repo.empty {
			writeJSON(w, http.StatusConflict, map[string]string{"message": "Git Repository is empty."})
			return
		}
		var in struct {
			Tree []*github.TreeEntry `json:"tree"`
		}
		decode(r, &in)
		sha := repo.next("t")
		repo.trees[sha] = in.Tree
		writeJSON(w, 201, map[string]any{"sha": sha, "tree": in.Tree})
	}))
	mux.HandleFunc("POST /repos/{owner}/{repo}/git/commits", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		var in struct {
			Message string `json:"message"`
			Tree    string `json:"tree"` // the tree's SHA, as go-github sends it
		}
		decode(r, &in)
		sha := repo.next("c")
		repo.commits[sha] = fakeCommit{tree: in.Tree, message: in.Message}
		writeJSON(w, 201, map[string]string{"sha": sha})
	}))
	mux.HandleFunc("GET /repos/{owner}/{repo}/actions/permissions/workflow", f.withRepo(func(w http.ResponseWriter, _ *http.Request, repo *fakeRepo) {
		writeJSON(w, 200, map[string]any{"default_workflow_permissions": repo.workflowPerm, "can_approve_pull_request_reviews": false})
	}))
	mux.HandleFunc("PUT /repos/{owner}/{repo}/actions/permissions/workflow", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		var in github.DefaultWorkflowPermissionRepository
		decode(r, &in)
		repo.workflowPerm = in.GetDefaultWorkflowPermissions()
		w.WriteHeader(204)
	}))
	mux.HandleFunc("GET /orgs/{org}/teams/{slug}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		t, ok := f.orgTeams[r.PathValue("slug")]
		if !ok {
			notFound(w, "Not Found")
			return
		}
		writeJSON(w, 200, t)
	})
	mux.HandleFunc("GET /repos/{owner}/{repo}/teams", f.withRepo(func(w http.ResponseWriter, _ *http.Request, repo *fakeRepo) {
		teams := []map[string]string{}
		for slug, perm := range repo.teams {
			teams = append(teams, map[string]string{"slug": slug, "permission": perm})
		}
		writeJSON(w, 200, teams)
	}))
	mux.HandleFunc("PUT /orgs/{org}/teams/{slug}/repos/{owner}/{repo}", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		var in github.AddTeamRepoRequest
		decode(r, &in)
		repo.teams[strings.ToLower(r.PathValue("slug"))] = in.GetPermission()
		w.WriteHeader(204)
	}))
	mux.HandleFunc("GET /repos/{owner}/{repo}/collaborators/{login}/permission", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		login := r.PathValue("login")
		permission, ok := repo.collaborators[login]
		if !ok {
			permission = f.permission
		}
		writeJSON(w, 200, map[string]any{"permission": permission, "user": map[string]string{"login": login}})
	}))
	// An archived repository's collaborators are read-only: GitHub answers
	// a grant or a revocation with 403 (checked live 2026-09-24).
	mux.HandleFunc("PUT /repos/{owner}/{repo}/collaborators/{login}", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		if repo.archived {
			archivedReadOnly(w)
			return
		}
		var in github.AddCollaboratorRequest
		decode(r, &in)
		repo.collaborators[r.PathValue("login")] = in.GetPermission()
		writeJSON(w, 201, map[string]any{})
	}))
	mux.HandleFunc("DELETE /repos/{owner}/{repo}/collaborators/{login}", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		if repo.archived {
			archivedReadOnly(w)
			return
		}
		delete(repo.collaborators, r.PathValue("login"))
		w.WriteHeader(204)
	}))
	mux.HandleFunc("POST /repos/{owner}/{repo}/branches/{branch}/rename", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		var in struct {
			NewName string `json:"new_name"`
		}
		decode(r, &in)
		if repo.hasBranch(in.NewName) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"message": "Validation Failed", "errors": []map[string]string{{"message": "New branch already exists"}}})
			return
		}
		if r.PathValue("branch") == repo.defaultBranch {
			repo.defaultBranch = in.NewName
		}
		writeJSON(w, 201, map[string]any{"name": in.NewName})
	}))
	mux.HandleFunc("GET /repos/{owner}/{repo}/branches/{branch}", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		if !repo.hasBranch(r.PathValue("branch")) {
			notFound(w, "Branch not found")
			return
		}
		writeJSON(w, 200, map[string]any{"name": r.PathValue("branch")})
	}))
	mux.HandleFunc("GET /repos/{owner}/{repo}/branches/{branch}/protection", f.withRepo(func(w http.ResponseWriter, _ *http.Request, repo *fakeRepo) {
		if repo.protectionStatus != 0 {
			writeJSON(w, repo.protectionStatus, map[string]string{"message": "Resource not accessible by integration"})
			return
		}
		p := repo.protection
		if p == nil {
			notFound(w, "Branch not protected")
			return
		}
		checks := []map[string]any{}
		for _, c := range p.checks {
			checks = append(checks, map[string]any{"context": c, "app_id": nil})
		}
		writeJSON(w, 200, map[string]any{
			"required_status_checks":        map[string]any{"strict": p.strict, "contexts": p.checks, "checks": checks},
			"enforce_admins":                map[string]bool{"enabled": p.enforceAdmins},
			"required_pull_request_reviews": map[string]int{"required_approving_review_count": p.reviews},
			"allow_force_pushes":            map[string]bool{"enabled": p.allowForce},
			"allow_deletions":               map[string]bool{"enabled": p.allowDel},
		})
	}))
	mux.HandleFunc("PUT /repos/{owner}/{repo}/branches/{branch}/protection", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		var in github.ProtectionRequest
		decode(r, &in)
		p := &fakeProtection{enforceAdmins: in.EnforceAdmins}
		if in.RequiredPullRequestReviews != nil {
			p.reviews = in.RequiredPullRequestReviews.RequiredApprovingReviewCount
		}
		if in.AllowForcePushes != nil {
			p.allowForce = *in.AllowForcePushes
		}
		if in.AllowDeletions != nil {
			p.allowDel = *in.AllowDeletions
		}
		if in.RequiredStatusChecks != nil {
			p.strict = in.RequiredStatusChecks.Strict
			if in.RequiredStatusChecks.Checks != nil {
				for _, c := range *in.RequiredStatusChecks.Checks {
					p.checks = append(p.checks, c.Context)
				}
			}
		}
		repo.protection = p
		repo.protected = r.PathValue("branch")
		writeJSON(w, 200, map[string]any{})
	}))
	mux.HandleFunc("DELETE /repos/{owner}/{repo}/branches/{branch}/protection", f.withRepo(func(w http.ResponseWriter, _ *http.Request, repo *fakeRepo) {
		if repo.protection == nil {
			notFound(w, "Branch not protected")
			return
		}
		repo.protection = nil
		w.WriteHeader(204)
	}))
	// The rulesets list carries the summary alone, as GitHub's does: the
	// rules, conditions and bypass actors need the ruleset itself.
	mux.HandleFunc("GET /repos/{owner}/{repo}/rulesets", f.withRepo(func(w http.ResponseWriter, _ *http.Request, repo *fakeRepo) {
		list := []map[string]any{}
		for _, rs := range repo.rulesets {
			list = append(list, map[string]any{"id": rs.GetID(), "name": rs.Name, "target": rs.GetTarget(), "source_type": "Repository", "source": repo.owner + "/" + repo.name, "enforcement": rs.Enforcement})
		}
		writeJSON(w, 200, list)
	}))
	mux.HandleFunc("GET /repos/{owner}/{repo}/rulesets/{id}", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		i := repo.rulesetIndex(r.PathValue("id"))
		if i < 0 {
			notFound(w, "Not Found")
			return
		}
		writeJSON(w, 200, rulesetJSON(repo.rulesets[i], f.readIdentity))
	}))
	mux.HandleFunc("POST /repos/{owner}/{repo}/rulesets", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		var in github.RepositoryRuleset
		decode(r, &in)
		if f.refusesBypass(w, in.BypassActors) {
			return
		}
		repo.seq++
		in.ID = new(int64(repo.seq))
		repo.rulesets = append(repo.rulesets, &in)
		writeJSON(w, 201, in)
	}))
	mux.HandleFunc("PUT /repos/{owner}/{repo}/rulesets/{id}", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		i := repo.rulesetIndex(r.PathValue("id"))
		if i < 0 {
			notFound(w, "Not Found")
			return
		}
		var in github.RepositoryRuleset
		decode(r, &in)
		if f.refusesBypass(w, in.BypassActors) {
			return
		}
		in.ID = repo.rulesets[i].ID
		repo.rulesets[i] = &in
		writeJSON(w, 200, in)
	}))
	mux.HandleFunc("DELETE /repos/{owner}/{repo}/rulesets/{id}", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		i := repo.rulesetIndex(r.PathValue("id"))
		if i < 0 {
			notFound(w, "Not Found")
			return
		}
		repo.rulesets = slices.Delete(repo.rulesets, i, i+1)
		w.WriteHeader(204)
	}))
	// Deploy keys: an archived repository still lets one go (checked live
	// 2026-09-24).
	mux.HandleFunc("GET /repos/{owner}/{repo}/keys", f.withRepo(func(w http.ResponseWriter, _ *http.Request, repo *fakeRepo) {
		if repo.keysStatus != 0 {
			writeJSON(w, repo.keysStatus, map[string]any{"message": "Not Found"})
			return
		}
		keys := repo.deployKeys
		if keys == nil {
			keys = []*github.Key{}
		}
		writeJSON(w, 200, keys)
	}))
	mux.HandleFunc("DELETE /repos/{owner}/{repo}/keys/{id}", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		for i, k := range repo.deployKeys {
			if strconv.FormatInt(k.GetID(), 10) == r.PathValue("id") {
				repo.deployKeys = slices.Delete(repo.deployKeys, i, i+1)
				w.WriteHeader(204)
				return
			}
		}
		notFound(w, "Not Found")
	}))
	mux.HandleFunc("GET /repos/{owner}/{repo}/hooks", f.withRepo(func(w http.ResponseWriter, _ *http.Request, repo *fakeRepo) {
		if repo.hooksStatus != 0 {
			writeJSON(w, repo.hooksStatus, map[string]any{"message": "Not Found"})
			return
		}
		hooks := repo.hooks
		if hooks == nil {
			hooks = []*github.Hook{}
		}
		writeJSON(w, 200, hooks)
	}))
	mux.HandleFunc("POST /repos/{owner}/{repo}/hooks", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		var in github.Hook
		decode(r, &in)
		in.ID = new(int64(len(repo.hooks) + 1))
		in.Config.Secret = nil
		repo.hooks = append(repo.hooks, &in)
		writeJSON(w, 201, in)
	}))
	mux.HandleFunc("PATCH /repos/{owner}/{repo}/hooks/{id}", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		var in github.Hook
		decode(r, &in)
		for i, h := range repo.hooks {
			if h.GetID() == id {
				in.ID = h.ID
				in.Config.Secret = nil
				repo.hooks[i] = &in
			}
		}
		writeJSON(w, 200, in)
	}))
	mux.HandleFunc("GET /user/installations/{id}/repositories", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.installation.status != http.StatusOK {
			writeJSON(w, f.installation.status, map[string]string{"message": "Resource not accessible by integration"})
			return
		}
		repos := []map[string]string{}
		for _, slug := range f.installation.repos {
			repos = append(repos, map[string]string{"full_name": slug})
		}
		writeJSON(w, 200, map[string]any{"total_count": len(repos), "repository_selection": f.installation.selection, "repositories": repos})
	})
	mux.HandleFunc("GET /repos/{owner}/{repo}/releases/latest", f.withRepo(func(w http.ResponseWriter, _ *http.Request, repo *fakeRepo) {
		if repo.release == "" {
			notFound(w, "Not Found")
			return
		}
		writeJSON(w, 200, map[string]any{"tag_name": repo.release, "created_at": repo.releaseAt.Format(time.RFC3339), "published_at": repo.releaseAt.Format(time.RFC3339)})
	}))
	mux.HandleFunc("GET /repos/{owner}/{repo}/releases", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		list := repo.releases
		if size := repo.releasesPage; size > 0 {
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			page = max(page, 1)
			start := min((page-1)*size, len(list))
			end := min(start+size, len(list))
			if end < len(list) {
				w.Header().Set("Link", fmt.Sprintf(`<http://%s%s?page=%d>; rel="next"`, r.Host, r.URL.Path, page+1))
			}
			list = list[start:end]
		}
		out := []map[string]any{}
		for _, rel := range list {
			out = append(out, map[string]any{"tag_name": rel.tag, "created_at": rel.at.Format(time.RFC3339), "published_at": rel.at.Format(time.RFC3339), "draft": rel.draft, "prerelease": rel.prerelease})
		}
		writeJSON(w, 200, out)
	}))
	mux.HandleFunc("GET /repos/{owner}/{repo}/actions/workflows/{file}/runs", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		if !f.mayDispatch(r) {
			forbidden(w)
			return
		}
		runs := []map[string]any{}
		for i, status := range f.runs[repo.owner+"/"+repo.name+"/"+r.PathValue("file")] {
			runs = append(runs, map[string]any{"run_number": i + 1, "status": status})
		}
		writeJSON(w, 200, map[string]any{"total_count": len(runs), "workflow_runs": runs})
	}))
	mux.HandleFunc("POST /repos/{owner}/{repo}/actions/workflows/{file}/dispatches", f.withRepo(func(w http.ResponseWriter, r *http.Request, repo *fakeRepo) {
		if !f.mayDispatch(r) {
			forbidden(w)
			return
		}
		var in github.CreateWorkflowDispatchEventRequest
		decode(r, &in)
		f.dispatches = append(f.dispatches, r.PathValue("file"))
		f.dispatchedBy = append(f.dispatchedBy, bearer(r))
		if f.onDispatch != nil {
			f.onDispatch(r.PathValue("file"), in.Inputs)
		}
		w.WriteHeader(204)
	}))
}

// fakeCircleCI is the CircleCI fake: the read surface of a person's client,
// a project with its pipeline definitions and their triggers. The engine
// writes nothing on CircleCI, so every non-GET request is a recorded
// mutation a test asserts away.
type fakeCircleCI struct {
	mu        sync.Mutex
	projects  map[string]*fakeProject // org/repo
	mutations []string
	srv       *httptest.Server
}

// fakeProject is one CircleCI project: its id and pipeline definitions.
type fakeProject struct {
	id          string
	definitions []*fakeDefinition
}

// fakeDefinition is a pipeline definition and its triggers.
type fakeDefinition struct {
	def      circleciclient.PipelineDefinition
	triggers []circleciclient.Trigger
}

func newFakeCircleCI() *fakeCircleCI {
	f := &fakeCircleCI{projects: map[string]*fakeProject{}}
	mux := http.NewServeMux()
	f.routes(mux)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		if r.Method != http.MethodGet {
			f.mutations = append(f.mutations, r.Method+" "+r.URL.Path)
		}
		f.mu.Unlock()
		mux.ServeHTTP(w, r)
	}))
	return f
}

// project returns the project of org/repo, created empty (no definition)
// when the fake does not know it yet.
func (f *fakeCircleCI) project(org, repo string) *fakeProject {
	slug := org + "/" + repo
	if p, ok := f.projects[slug]; ok {
		return p
	}
	p := &fakeProject{id: "project-" + org + "-" + repo}
	f.projects[slug] = p
	return p
}

// addDefinition adds a definition named name of provider to the project and
// returns it; trigger, when set, is its one trigger.
func (p *fakeProject) addDefinition(name, provider string, trigger *circleciclient.Trigger) *fakeDefinition {
	d := &fakeDefinition{def: circleciclient.PipelineDefinition{
		ID:           fmt.Sprintf("%s-def-%d", p.id, len(p.definitions)+1),
		Name:         name,
		ConfigSource: circleciclient.DefinitionSource{Provider: provider, FilePath: circleciclient.ConfigFile},
	}}
	if trigger != nil {
		d.triggers = append(d.triggers, *trigger)
	}
	p.definitions = append(p.definitions, d)
	return d
}

// addApp seeds the project of a GitHub App pipeline as `devctl repo
// follow-circleci` leaves it: the implicit OAuth definition without a
// trigger beside the GitHub App definition with its all-pushes trigger.
func (f *fakeCircleCI) addApp(org, repo string) *fakeProject {
	p := f.project(org, repo)
	p.addDefinition(repo, circleciclient.ProviderGitHubOAuth, nil)
	trigger := circleciclient.AllPushesTrigger("1")
	p.addDefinition("build", circleciclient.ProviderGitHubApp, &trigger)
	return p
}

// addOAuth seeds a project that builds through its OAuth definition: the
// implicit definition with an enabled trigger.
func (f *fakeCircleCI) addOAuth(org, repo string) *fakeProject {
	p := f.project(org, repo)
	p.addDefinition(repo, circleciclient.ProviderGitHubOAuth, &circleciclient.Trigger{
		EventSource: circleciclient.DefinitionSource{Provider: circleciclient.ProviderGitHubOAuth},
	})
	return p
}

func (f *fakeCircleCI) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v2/project/gh/{org}/{repo}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		p, ok := f.projects[r.PathValue("org")+"/"+r.PathValue("repo")]
		if !ok {
			notFound(w, "Project not found")
			return
		}
		writeJSON(w, 200, circleciclient.Project{Slug: "gh/" + r.PathValue("org") + "/" + r.PathValue("repo"), Name: r.PathValue("repo"), ID: p.id})
	})
	mux.HandleFunc("GET /api/v2/projects/{id}/pipeline-definitions", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		items := []circleciclient.PipelineDefinition{}
		for _, p := range f.projects {
			if p.id == r.PathValue("id") {
				for _, d := range p.definitions {
					items = append(items, d.def)
				}
			}
		}
		writeJSON(w, 200, map[string]any{"items": items, "next_page_token": nil})
	})
	mux.HandleFunc("GET /api/v2/projects/{id}/pipeline-definitions/{def}/triggers", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		items := []circleciclient.Trigger{}
		for _, p := range f.projects {
			if p.id != r.PathValue("id") {
				continue
			}
			for _, d := range p.definitions {
				if d.def.ID == r.PathValue("def") {
					items = append(items, d.triggers...)
				}
			}
		}
		writeJSON(w, 200, map[string]any{"items": items})
	})
}

// circleCIDeployKeyID is the id of the deploy key an OAuth follow leaves on
// the GitHub repository.
const circleCIDeployKeyID = 164277013

// circleCIDeployKey is the deploy key CircleCI adds to a repository an OAuth
// project builds.
func circleCIDeployKey() *github.Key {
	return &github.Key{ID: new(int64(circleCIDeployKeyID)), Title: new(circleCIDeployKeyTitle), ReadOnly: new(true), Key: new("ssh-rsa AAAAB3NzaC1yc2E circleci")}
}

// jobState is a CircleCI job and the state of its status.
type jobState struct{ job, state string }

// addTagBuild seeds the statuses of the build of tag as the CircleCI GitHub
// App posts them: workflow's first status two seconds after the release was
// published (publishedAt), the jobs' own states a few seconds apart.
func (r *fakeRepo) addTagBuild(tag, workflow string, publishedAt time.Time, jobs ...jobState) {
	for i, j := range jobs {
		r.addStatus(tag, j.job, j.state, workflow, CircleCIAppLogin, publishedAt.Add(time.Duration(2+i)*time.Second))
	}
}

// fakeStatus is one commit status of a ref, as GitHub lists it.
type fakeStatus struct {
	context, state, targetURL, creator string
	at                                 time.Time
}

// addStatus seeds a CircleCI-style status on ref (a branch or a tag): job is
// the CircleCI job's name, workflow the workflow its target URL names,
// creator the login that posted it. Statuses are listed newest first.
func (r *fakeRepo) addStatus(ref, job, state, workflow, creator string, at time.Time) {
	if r.refStatuses == nil {
		r.refStatuses = map[string][]fakeStatus{}
	}
	s := fakeStatus{context: CircleCIContextPrefix + job, state: state, creator: creator, at: at,
		targetURL: "https://app.circleci.com/workflow/" + workflow + "/job/" + job}
	list := append(r.refStatuses[ref], s)
	sort.SliceStable(list, func(i, j int) bool { return list[i].at.After(list[j].at) })
	r.refStatuses[ref] = list
}

// scaffoldFiles is what the fake renderer renders and what a seeded
// repository carries: the files the steps read.
var scaffoldFiles = map[string]string{
	"README.md":      "# sample-service\n",
	"CODEOWNERS":     reposetup.Codeowners("team-bumblebee"),
	"Makefile":       "include Makefile.*.mk\n",
	"renovate.json5": "{\n  extends: ['github>giantswarm/renovate-presets:default.json5'],\n}\n",
	// The generated pipeline: the setup config continues into workflows.yml.
	".circleci/config.yml": "version: 2.1\nsetup: true\n",
	".circleci/workflows.yml": `version: 2.1
workflows:
  build:
    jobs:
    - architect/go-build:
        name: go-build
        filters:
          tags:
            only: /^v.*/
    - architect/push-to-registries:
        name: push-to-registries-release
        filters:
          branches:
            ignore: /.*/
          tags:
            only: /^v.*/
`,
	"helm/sample-service/Chart.yaml": `apiVersion: v2
name: sample-service
version: 0.0.1
icon: ` + defaultChartIcon + `
annotations:
  io.giantswarm.application.team: bumblebee
`,
	"helm/sample-service/values.schema.json": "{}\n",
}

// fakeRenderer writes scaffoldFiles, or fails.
type fakeRenderer struct {
	fail error
}

func (f fakeRenderer) Render(_ context.Context, req reposetup.RenderRequest) (*reposetup.Scaffold, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	var files []string
	for p, c := range scaffoldFiles {
		full := filepath.Join(req.Dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			return nil, err
		}
		if err := os.WriteFile(full, []byte(c), 0o600); err != nil {
			return nil, err
		}
		files = append(files, p)
	}
	sort.Strings(files)
	return &reposetup.Scaffold{Dir: req.Dir, Template: req.Entry.Template, Chart: req.Entry.Chart, Files: files}, nil
}
