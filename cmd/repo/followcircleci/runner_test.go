package followcircleci

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/go-github/v93/github"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/circleciapp"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

// fakeCircleCI is a project with the implicit OAuth definition, or with a
// GitHub App pipeline set up already; its pipelines end in workflowStatus.
type fakeCircleCI struct {
	setUp          bool
	refuse         int // the HTTP status of the first write's refusal; 0 accepts
	workflowStatus string
	writes         []string
}

func (f *fakeCircleCI) GetProject(context.Context, string, string) (*circleciclient.Project, error) {
	return &circleciclient.Project{ID: "p1"}, nil
}

func (f *fakeCircleCI) ListPipelineDefinitions(context.Context, string) ([]circleciclient.PipelineDefinition, error) {
	defs := []circleciclient.PipelineDefinition{{ID: "oauth", ConfigSource: circleciclient.DefinitionSource{Provider: circleciclient.ProviderGitHubOAuth}}}
	if f.setUp {
		app := circleciclient.AppDefinition("mcp-template-app", "", "42")
		app.ID = "app"
		defs = append(defs, app)
	}
	return defs, nil
}

func (f *fakeCircleCI) CreatePipelineDefinition(_ context.Context, _ string, def circleciclient.PipelineDefinition) (*circleciclient.PipelineDefinition, error) {
	if f.refuse != 0 {
		return nil, apiError(f.refuse)
	}
	f.writes = append(f.writes, "definition "+def.Name)
	def.ID = "app"
	return &def, nil
}

func (f *fakeCircleCI) ListTriggers(_ context.Context, _, definitionID string) ([]circleciclient.Trigger, error) {
	switch {
	case f.setUp && definitionID == "app":
		return []circleciclient.Trigger{{ID: "on"}}, nil
	case f.setUp && definitionID == "oauth":
		return []circleciclient.Trigger{{ID: "implicit"}}, nil
	}
	return nil, nil
}

func (f *fakeCircleCI) CreateTrigger(_ context.Context, _, _ string, t circleciclient.Trigger) (*circleciclient.Trigger, error) {
	f.writes = append(f.writes, "trigger "+t.EventPreset)
	return &t, nil
}

func (f *fakeCircleCI) DeleteTrigger(context.Context, string, string) error { return nil }

func (f *fakeCircleCI) RunPipeline(_ context.Context, _, _, _ string, ref circleciclient.Ref) (*circleciclient.Pipeline, error) {
	f.writes = append(f.writes, "run "+ref.String())
	return &circleciclient.Pipeline{ID: "pl", Number: 1}, nil
}

func (f *fakeCircleCI) GetPipeline(context.Context, string) (*circleciclient.PipelineDetail, error) {
	return &circleciclient.PipelineDetail{Pipeline: circleciclient.Pipeline{State: "created"}}, nil
}

func (f *fakeCircleCI) ListPipelineWorkflows(context.Context, string) ([]circleciclient.Workflow, error) {
	return []circleciclient.Workflow{{Name: "build", Status: f.workflowStatus}}, nil
}

// apiError is CircleCI's error for an answer of code.
func apiError(code int) error {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }))
	defer srv.Close()
	c, _ := circleciclient.New(circleciclient.Config{Token: "t", BaseURL: srv.URL})
	_, err := c.GetProject(context.Background(), "giantswarm", "mcp-template")
	return err
}

type fakeGitHub struct{}

func (fakeGitHub) Repository(context.Context, string, string) (int64, string, error) {
	return 42, "main", nil
}

func (fakeGitHub) HasConfig(context.Context, string, string, string) (bool, error) {
	return true, nil
}

func (fakeGitHub) LatestRelease(context.Context, string, string) (*github.RepositoryRelease, error) {
	return nil, nil
}

func (fakeGitHub) CircleCIStatuses(context.Context, string, string, string) ([]*github.RepoStatus, error) {
	return nil, nil
}

func newRunner(ci *fakeCircleCI, out *bytes.Buffer) *runner {
	return &runner{
		flag:   &flag{Wait: true, Timeout: time.Minute},
		stdout: out,
		stderr: out,
		open: func(context.Context, string, func(string)) (circleciapp.GitHub, circleciapp.CircleCI, error) {
			return fakeGitHub{}, ci, nil
		},
		sleep: func(context.Context, time.Duration) error { return nil },
	}
}

func TestRun(t *testing.T) {
	t.Run("sets up, runs the default branch and waits green", func(t *testing.T) {
		var out bytes.Buffer
		ci := &fakeCircleCI{workflowStatus: "success"}
		require.NoError(t, newRunner(ci, &out).run(context.Background(), "mcp-template"))
		require.Equal(t, []string{"definition mcp-template-app", "trigger all-pushes", "run branch main"}, ci.writes)
		require.Contains(t, out.String(), "giantswarm/mcp-template: pipeline 1 (branch main) green")
	})
	t.Run("set up already: nothing to do", func(t *testing.T) {
		var out bytes.Buffer
		ci := &fakeCircleCI{setUp: true}
		require.NoError(t, newRunner(ci, &out).run(context.Background(), "giantswarm/mcp-template"))
		require.Empty(t, ci.writes)
		require.Contains(t, out.String(), "set up already, nothing to do")
		require.Contains(t, out.String(), "the OAuth pipeline is still triggered")
	})
	t.Run("a red run fails the command, the pipeline stays", func(t *testing.T) {
		var out bytes.Buffer
		ci := &fakeCircleCI{workflowStatus: "failed"}
		err := newRunner(ci, &out).run(context.Background(), "mcp-template")
		require.Error(t, err)
		require.Contains(t, err.Error(), "pipeline 1 (branch main) red")
	})
	t.Run("a Read-only login asks for a login granting Write", func(t *testing.T) {
		var out bytes.Buffer
		err := newRunner(&fakeCircleCI{refuse: http.StatusForbidden}, &out).run(context.Background(), "mcp-template")
		var authErr *authstore.AuthRequiredError
		require.ErrorAs(t, err, &authErr)
		require.Contains(t, err.Error(), "grant Write access")
	})
	t.Run("a malformed argument", func(t *testing.T) {
		var out bytes.Buffer
		err := newRunner(&fakeCircleCI{}, &out).run(context.Background(), "giantswarm/a/b")
		require.Error(t, err)
	})
}
