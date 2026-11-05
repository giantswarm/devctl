package circleciclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The answers of CircleCI as read on release-automation-sandbox on
// 2026-11-02: an OAuth project with a GitHub App definition beside its
// implicit one.
const (
	definitionsAnswer = `{"items":[{"id":"eaf2b565","name":"sandbox-app","created_at":"2026-10-22T11:11:20.446946Z","config_source":{"provider":"github_app","repo":{"full_name":"giantswarm/sandbox","external_id":"1417210242"},"file_path":".circleci/config.yml"},"checkout_source":{"provider":"github_app","repo":{"full_name":"giantswarm/sandbox","external_id":"1417210242"}}},{"id":"6877ac71","name":"sandbox","description":"Implicit pipeline definition associated with an OAuth-based project.","config_source":{"provider":"github_oauth","repo":{"full_name":"giantswarm/sandbox","external_id":"1417210242"},"file_path":".circleci/config.yml"},"checkout_source":{"provider":"github_oauth","repo":{"full_name":"giantswarm/sandbox","external_id":"1417210242"}}}]}`
	triggerAnswer     = `{"id":"2a6debf2","name":"","event_name":"","checkout_ref":"","config_ref":"","event_source":{"provider":"github_app","repo":{"full_name":"giantswarm/sandbox","external_id":"1417210242"}},"event_preset":"all-pushes","disabled":false}`
)

type recorded struct {
	method, path string
	body         map[string]any
}

func recordingServer(t *testing.T, answers map[string]string) (*Client, *[]recorded) {
	t.Helper()
	var calls []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := recorded{method: r.Method, path: r.URL.Path}
		if b, _ := io.ReadAll(r.Body); len(b) > 0 {
			_ = json.Unmarshal(b, &c.body)
		}
		calls = append(calls, c)
		answer, ok := answers[r.Method+" "+r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Route Not Found."}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(answer))
	}))
	t.Cleanup(srv.Close)
	c, err := New(Config{Token: "cci_test", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	return c, &calls
}

// TestListPipelineDefinitionsTellsTheAppFromOAuth: the listing reads both
// definitions, and only the github_app one is the App's.
func TestListPipelineDefinitionsTellsTheAppFromOAuth(t *testing.T) {
	c, _ := recordingServer(t, map[string]string{"GET /api/v2/projects/p1/pipeline-definitions": definitionsAnswer})
	defs, err := c.ListPipelineDefinitions(context.Background(), "p1")
	if err != nil || len(defs) != 2 {
		t.Fatalf("list: %+v %v", defs, err)
	}
	if !defs[0].GitHubApp() || defs[1].GitHubApp() || defs[0].ConfigSource.Repo.ExternalID != "1417210242" {
		t.Errorf("definitions read: %+v", defs)
	}
}

// TestCreateAppPipelineSendsTheSandboxShape: the definition and the trigger
// are posted in the shape CircleCI accepted on 2026-11-02 from a devctl
// login: github_app config and checkout naming the repository by GitHub id,
// the config file on the config source alone, and an all-pushes trigger.
func TestCreateAppPipelineSendsTheSandboxShape(t *testing.T) {
	c, calls := recordingServer(t, map[string]string{
		"POST /api/v2/projects/p1/pipeline-definitions":             `{"id":"d1","name":"sandbox-app"}`,
		"POST /api/v2/projects/p1/pipeline-definitions/d1/triggers": triggerAnswer,
	})
	def, err := c.CreatePipelineDefinition(context.Background(), "p1", AppDefinition("sandbox-app", "", "1417210242"))
	if err != nil || def.ID != "d1" {
		t.Fatalf("create definition: %+v %v", def, err)
	}
	trigger, err := c.CreateTrigger(context.Background(), "p1", "d1", AllPushesTrigger("1417210242"))
	if err != nil || trigger.ID != "2a6debf2" || trigger.EventPreset != EventPresetAllPushes {
		t.Fatalf("create trigger: %+v %v", trigger, err)
	}
	got, _ := json.Marshal((*calls)[0].body)
	want := `{"checkout_source":{"provider":"github_app","repo":{"external_id":"1417210242"}},"config_source":{"file_path":".circleci/config.yml","provider":"github_app","repo":{"external_id":"1417210242"}},"name":"sandbox-app"}`
	if string(got) != want {
		t.Errorf("definition body:\n got %s\nwant %s", got, want)
	}
	got, _ = json.Marshal((*calls)[1].body)
	want = `{"event_preset":"all-pushes","event_source":{"provider":"github_app","repo":{"external_id":"1417210242"}}}`
	if string(got) != want {
		t.Errorf("trigger body:\n got %s\nwant %s", got, want)
	}
}

// TestDeleteTriggerAddressesTheProject: a trigger is deleted under the
// project; the definition's path is a route CircleCI does not have.
func TestDeleteTriggerAddressesTheProject(t *testing.T) {
	c, calls := recordingServer(t, map[string]string{"DELETE /api/v2/projects/p1/triggers/t1": ``})
	if err := c.DeleteTrigger(context.Background(), "p1", "t1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(*calls) != 1 || (*calls)[0].method != http.MethodDelete {
		t.Errorf("calls: %+v", *calls)
	}
}

// TestRunPipelineNamesTheDefinitionAndRef: a run posts the definition with
// the ref as both config and checkout, and a ref must be one of branch and
// tag.
func TestRunPipelineNamesTheDefinitionAndRef(t *testing.T) {
	c, calls := recordingServer(t, map[string]string{
		"POST /api/v2/project/gh/giantswarm/sandbox/pipeline/run": `{"state":"setup-pending","created_at":"2026-11-02T16:05:37.832Z","number":137,"id":"a53502dd"}`,
	})
	p, err := c.RunPipeline(context.Background(), "giantswarm", "sandbox", "d1", Ref{Branch: "main"})
	if err != nil || p.Number != 137 || p.VCS.Branch != "main" {
		t.Fatalf("run: %+v %v", p, err)
	}
	got, _ := json.Marshal((*calls)[0].body)
	if want := `{"checkout":{"branch":"main"},"config":{"branch":"main"},"definition_id":"d1"}`; string(got) != want {
		t.Errorf("run body:\n got %s\nwant %s", got, want)
	}
	for _, ref := range []Ref{{}, {Branch: "main", Tag: "v1.0.0"}} {
		if _, err := c.RunPipeline(context.Background(), "giantswarm", "sandbox", "d1", ref); !IsInvalidConfig(err) {
			t.Errorf("ref %+v: want an invalid config error, got %v", ref, err)
		}
	}
}

// TestListTriggersPages: the triggers are read page by page, as the
// definitions are.
func TestListTriggersPages(t *testing.T) {
	var pages []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("page-token")
		pages = append(pages, token)
		w.Header().Set("Content-Type", "application/json")
		if token == "" {
			_, _ = w.Write([]byte(`{"items":[{"id":"t1","event_preset":"all-pushes"}],"next_page_token":"p2"}`))
			return
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"t2","event_preset":"all-pushes","disabled":true}],"next_page_token":null}`))
	}))
	defer srv.Close()
	c, err := New(Config{Token: "cci_test", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	triggers, err := c.ListTriggers(context.Background(), "p1", "d1")
	if err != nil || len(triggers) != 2 || triggers[1].ID != "t2" || !triggers[1].Disabled {
		t.Fatalf("triggers: %+v %v", triggers, err)
	}
	if len(pages) != 2 || pages[1] != "p2" {
		t.Errorf("pages read: %q", pages)
	}
}
