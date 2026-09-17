package reposetup

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-github/v92/github"
)

// fakeTeamFiles is giantswarm/github's REST surface as Remote uses it:
// the repositories/ listing, the team files, the caller, the caller's
// teams, and the branch, commit and pull request of one change.
type fakeTeamFiles struct {
	files map[string]string // team → content
	// what OpenPullRequest sent
	branch string
	commit map[string]any
	pull   map[string]any
}

func (f *fakeTeamFiles) handler(t *testing.T) http.Handler {
	writeJSON := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/giantswarm/github/contents/repositories", func(w http.ResponseWriter, r *http.Request) {
		var dir []map[string]any
		for team := range f.files {
			dir = append(dir, map[string]any{"type": "file", "name": team + ".yaml", "path": "repositories/" + team + ".yaml"})
		}
		dir = append(dir, map[string]any{"type": "file", "name": "README.md"})
		writeJSON(w, dir)
	})
	mux.HandleFunc("GET /repos/giantswarm/github/contents/repositories/{file}", func(w http.ResponseWriter, r *http.Request) {
		team := strings.TrimSuffix(r.PathValue("file"), ".yaml")
		content, ok := f.files[team]
		if !ok {
			w.WriteHeader(404)
			return
		}
		writeJSON(w, map[string]any{"type": "file", "sha": "sha-" + team, "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(content))})
	})
	mux.HandleFunc("GET /user", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"login": "octocat"})
	})
	mux.HandleFunc("GET /user/teams", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []map[string]any{
			{"slug": "team-bumblebee", "organization": map[string]any{"login": "giantswarm"}},
			{"slug": "team-other-org", "organization": map[string]any{"login": "elsewhere"}},
		})
	})
	mux.HandleFunc("GET /repos/giantswarm/github/git/ref/heads/main", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ref": "refs/heads/main", "object": map[string]any{"sha": "main-sha"}})
	})
	mux.HandleFunc("POST /repos/giantswarm/github/git/refs", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["sha"] != "main-sha" {
			t.Errorf("branch created from %q, want main-sha", body["sha"])
		}
		if f.branch != "" {
			w.WriteHeader(422)
			return
		}
		f.branch = body["ref"]
		w.WriteHeader(201)
		writeJSON(w, body)
	})
	mux.HandleFunc("PUT /repos/giantswarm/github/contents/repositories/{file}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&f.commit)
		writeJSON(w, map[string]any{"content": map[string]any{"path": "repositories/" + r.PathValue("file")}})
	})
	mux.HandleFunc("POST /repos/giantswarm/github/pulls", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&f.pull)
		w.WriteHeader(201)
		writeJSON(w, map[string]any{"number": 7, "html_url": "https://github.com/giantswarm/github/pull/7"})
	})
	// The enterprise client prefixes every path with /api/v3.
	return http.StripPrefix("/api/v3", mux)
}

func newRemote(t *testing.T, f *fakeTeamFiles) (Remote, func()) {
	srv := httptest.NewServer(f.handler(t))
	gh, err := github.NewClient(github.WithEnterpriseURLs(srv.URL, srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	return Remote{GitHub: gh}, srv.Close
}

func TestRemote(t *testing.T) {
	ctx := context.Background()
	f := &fakeTeamFiles{files: map[string]string{
		"team-bumblebee": "# header\n- name: alpha\n  componentType: service\n",
		"team-rocket":    "- name: rocket-thing\n  componentType: service\n",
	}}
	remote, done := newRemote(t, f)
	defer done()

	teams, err := remote.Teams(ctx)
	if err != nil || len(teams) != 2 {
		t.Fatalf("Teams: %v, %v", teams, err)
	}

	tf, err := remote.TeamFile(ctx, "team-bumblebee")
	if err != nil {
		t.Fatal(err)
	}
	if tf.Team != "team-bumblebee" || tf.SHA != "sha-team-bumblebee" || tf.Path != "repositories/team-bumblebee.yaml" || len(tf.Entries) != 1 {
		t.Errorf("TeamFile: %+v", tf)
	}
	if _, err := remote.TeamFile(ctx, "team-nobody"); !IsEntryNotFound(err) {
		t.Errorf("unknown team: got %v, want entryNotFoundError", err)
	}

	found, err := remote.FindEntry(ctx, "rocket-thing", nil)
	if err != nil || found.Team != "team-rocket" {
		t.Errorf("FindEntry: %v, %v", found, err)
	}
	if _, err := remote.FindEntry(ctx, "nowhere", nil); !IsEntryNotFound(err) {
		t.Errorf("undeclared: got %v, want entryNotFoundError", err)
	}

	person, err := remote.Person(ctx, "giantswarm")
	if err != nil || person.Login != "octocat" || strings.Join(person.Teams, ",") != "team-bumblebee" {
		t.Errorf("Person: %+v, %v", person, err)
	}

	pr, err := remote.OpenPullRequest(ctx, PullRequest{
		Branch: "repo-create/new", Path: tf.Path, Content: []byte("new content"), SHA: tf.SHA,
		Title: "feat(bumblebee): declare new", Body: "body",
	})
	if err != nil {
		t.Fatal(err)
	}
	if pr.GetHTMLURL() != "https://github.com/giantswarm/github/pull/7" {
		t.Errorf("pull request: %s", pr.GetHTMLURL())
	}
	if f.branch != "refs/heads/repo-create/new" {
		t.Errorf("branch: %s", f.branch)
	}
	content, _ := base64.StdEncoding.DecodeString(f.commit["content"].(string))
	if string(content) != "new content" || f.commit["sha"] != "sha-team-bumblebee" || f.commit["branch"] != "repo-create/new" || f.commit["message"] != "feat(bumblebee): declare new" {
		t.Errorf("commit: %v", f.commit)
	}
	if f.pull["head"] != "repo-create/new" || f.pull["base"] != "main" || f.pull["title"] != "feat(bumblebee): declare new" || f.pull["body"] != "body" {
		t.Errorf("pull: %v", f.pull)
	}

	// The same branch again is the change already proposed.
	if _, err := remote.OpenPullRequest(ctx, PullRequest{Branch: "repo-create/new", Path: tf.Path, Title: "t"}); !IsBranchExists(err) {
		t.Errorf("second branch: got %v, want branchExistsError", err)
	}
}

func TestCreationPullRequest(t *testing.T) {
	tf := &RemoteTeamFile{TeamFile: &TeamFile{Team: "team-bumblebee", Path: "repositories/team-bumblebee.yaml"}, SHA: "abc"}
	result := &Result{Entries: []Entry{{
		Name: "my-service", Rendered: "- name: my-service\n", Template: TemplateGo,
		NameCheck: NameCheck{Verdict: VerdictFree, Detail: "repository giantswarm/my-service does not exist"},
	}}, Notices: []Notice{{Kind: NoticeTeamReview, Message: "your team's review will be required"}}}

	pr := CreationPullRequest(tf, []byte("file"), result)
	if pr.Title != "feat(bumblebee): declare my-service" || pr.Branch != "repo-create/my-service" || pr.SHA != "abc" || pr.Path != tf.Path {
		t.Errorf("pull request: %+v", pr)
	}
	for _, want := range []string{"```yaml\n- name: my-service\n```", "Template: `giantswarm/template`", "Name check: free -- repository giantswarm/my-service does not exist", "- team-review: your team's review will be required"} {
		if !strings.Contains(pr.Body, want) {
			t.Errorf("body lacks %q:\n%s", want, pr.Body)
		}
	}
}
