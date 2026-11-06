package followcircleci

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/circleciapp"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

// fakeManager answers list_repositories with rows and records the call.
type fakeManager struct {
	rows []manager.Row
	args map[string]any
}

func (m *fakeManager) Call(_ context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	if tool != manager.ToolListRepositories {
		return nil, nil
	}
	m.args = args
	return json.Marshal(manager.Listing{Scope: "mine", Repositories: m.rows})
}

func newPending(m *fakeManager, ci *fakeCircleCI) *pendingPass {
	return &pendingPass{
		open: func(context.Context) (*client.Session, error) { return &client.Session{Caller: m}, nil },
		openCI: func(context.Context, string, func(string)) (circleciapp.GitHub, circleciapp.CircleCI, error) {
			return fakeGitHub{}, ci, nil
		},
	}
}

func TestPending_SetsUpTheDeclaredProductionRepositoriesOfYourTeams(t *testing.T) {
	m := &fakeManager{rows: []manager.Row{
		{Repository: "giantswarm/new-service", Team: "team-honeybadger"},
		{Repository: "giantswarm/prod-service", Team: "team-honeybadger", Lifecycle: "production"},
		{Repository: "giantswarm/old-service", Team: "team-honeybadger", Lifecycle: "deprecated"},
		{Repository: "giantswarm/nobodys"},
		{Repository: "giantswarm/frozen", Team: "team-honeybadger", Archived: true},
		{Repository: "giantswarm/vanished", Team: "team-honeybadger", Gone: true},
	}}
	ci := &fakeCircleCI{}

	summary, err := newPending(m, ci).run(t.Context())

	require.NoError(t, err)
	require.Equal(t, "mine", m.args["scope"])
	require.Equal(t, "circleci-pipeline-missing", m.args["finding"])
	require.NotContains(t, m.args, "lifecycle", "the manager's lifecycle filter knows no production")
	require.Equal(t, []string{
		"definition new-service-app", "trigger all-pushes", "run branch main",
		"definition prod-service-app", "trigger all-pushes", "run branch main",
	}, ci.writes)
	require.Equal(t, 2, summary.Pending)
	require.Equal(t, Entry{
		Repository: "giantswarm/new-service",
		Outcome:    OutcomeCreated,
		Runs:       []RunRef{{Ref: "branch main", Pipeline: 1}},
	}, summary.Repositories[0])
	require.Equal(t, "giantswarm/prod-service", summary.Repositories[1].Repository)
}

func TestPending_ReportsARepositorySetUpAlready(t *testing.T) {
	m := &fakeManager{rows: []manager.Row{{Repository: "giantswarm/mcp-template", Team: "team-honeybadger"}}}
	ci := &fakeCircleCI{setUp: true}

	summary, err := newPending(m, ci).run(t.Context())

	require.NoError(t, err)
	require.Empty(t, ci.writes)
	require.Equal(t, []Entry{{Repository: "giantswarm/mcp-template", Outcome: OutcomeAlreadySetUp}}, summary.Repositories)
}

func TestPending_EveryWriteRefusedAsksForWrite(t *testing.T) {
	m := &fakeManager{rows: []manager.Row{
		{Repository: "giantswarm/first", Team: "team-honeybadger"},
		{Repository: "giantswarm/second", Team: "team-honeybadger"},
	}}
	ci := &fakeCircleCI{refuse: http.StatusForbidden}

	summary, err := newPending(m, ci).run(t.Context())

	var authErr *authstore.AuthRequiredError
	require.ErrorAs(t, err, &authErr)
	require.Contains(t, err.Error(), "grant Write access")
	require.Contains(t, err.Error(), "a repository admin")
	require.Equal(t, 2, summary.Pending)
	require.Equal(t, []Outcome{OutcomeRefused, OutcomeRefused}, outcomes(summary))
}

// refusingRepository is CircleCI refusing the person the pipeline of one
// repository, as for a member who is no admin of it.
type refusingRepository struct {
	*fakeCircleCI
	repo string
}

func (f refusingRepository) CreatePipelineDefinition(ctx context.Context, projectID string, def circleciclient.PipelineDefinition) (*circleciclient.PipelineDefinition, error) {
	if def.Name == circleciapp.DefinitionName(f.repo) {
		return nil, apiError(http.StatusForbidden)
	}
	return f.fakeCircleCI.CreatePipelineDefinition(ctx, projectID, def)
}

func TestPending_ARefusalDoesNotStopTheOthersAndNamesTheAdmin(t *testing.T) {
	m := &fakeManager{rows: []manager.Row{
		{Repository: "giantswarm/aaa-not-mine-to-admin", Team: "team-honeybadger"},
		{Repository: "giantswarm/zzz-fine", Team: "team-honeybadger"},
	}}
	ci := &fakeCircleCI{}
	p := newPending(m, ci)
	p.openCI = func(context.Context, string, func(string)) (circleciapp.GitHub, circleciapp.CircleCI, error) {
		return fakeGitHub{}, refusingRepository{fakeCircleCI: ci, repo: "aaa-not-mine-to-admin"}, nil
	}

	summary, err := p.run(t.Context())

	require.Equal(t, []Outcome{OutcomeRefused, OutcomeCreated}, outcomes(summary))
	var authErr *authstore.AuthRequiredError
	require.False(t, errors.As(err, &authErr), "a write succeeded: the login grants Write")
	var exitErr *agentcli.ExitError
	require.ErrorAs(t, err, &exitErr)
	require.Equal(t, agentcli.ExitRefused, exitErr.Code)
	require.Contains(t, err.Error(), "a repository admin has to run `devctl repo follow-circleci giantswarm/aaa-not-mine-to-admin`")
}

func TestPending_AMissingGitHubLoginIsNamed(t *testing.T) {
	m := &fakeManager{rows: []manager.Row{{Repository: "giantswarm/new-service", Team: "team-honeybadger"}}}
	p := newPending(m, &fakeCircleCI{})
	p.openCI = func(context.Context, string, func(string)) (circleciapp.GitHub, circleciapp.CircleCI, error) {
		return nil, nil, &authstore.AuthRequiredError{Identity: "GitHub", Cause: "no token", Hint: "devctl auth login"}
	}

	summary, _ := p.run(t.Context())

	require.Equal(t, "no usable GitHub login", summary.Repositories[0].Reason)
}

func outcomes(s *Summary) []Outcome {
	var out []Outcome
	for _, e := range s.Repositories {
		out = append(out, e.Outcome)
	}
	return out
}

func TestPending_WithoutACircleCILoginSkipsEveryRepository(t *testing.T) {
	m := &fakeManager{rows: []manager.Row{
		{Repository: "giantswarm/first", Team: "team-honeybadger"},
		{Repository: "giantswarm/second", Team: "team-honeybadger"},
	}}
	p := newPending(m, &fakeCircleCI{})
	p.openCI = func(context.Context, string, func(string)) (circleciapp.GitHub, circleciapp.CircleCI, error) {
		return nil, nil, &authstore.AuthRequiredError{Identity: "CircleCI", Cause: "the token expired", Hint: "devctl auth login --circleci-only"}
	}

	summary, err := p.run(t.Context())

	var authErr *authstore.AuthRequiredError
	require.ErrorAs(t, err, &authErr)
	require.Equal(t, 2, summary.Pending)
	require.Equal(t, []Outcome{OutcomeSkipped, OutcomeSkipped}, outcomes(summary))
}

// failingProject is CircleCI failing to read the project of one repository.
type failingProject struct {
	*fakeCircleCI
	repo string
}

func (f failingProject) GetProject(ctx context.Context, owner, repo string) (*circleciclient.Project, error) {
	if repo == f.repo {
		return nil, apiError(http.StatusBadGateway)
	}
	return f.fakeCircleCI.GetProject(ctx, owner, repo)
}

func TestPending_AFailedRepositoryDoesNotStopTheOthers(t *testing.T) {
	m := &fakeManager{rows: []manager.Row{
		{Repository: "giantswarm/broken", Team: "team-honeybadger"},
		{Repository: "giantswarm/fine", Team: "team-honeybadger"},
	}}
	ci := &fakeCircleCI{}
	p := newPending(m, ci)
	p.openCI = func(context.Context, string, func(string)) (circleciapp.GitHub, circleciapp.CircleCI, error) {
		return fakeGitHub{}, failingProject{fakeCircleCI: ci, repo: "broken"}, nil
	}

	summary, err := p.run(t.Context())

	require.Error(t, err)
	require.Equal(t, []Outcome{OutcomeFailed, OutcomeCreated}, outcomes(summary))
	require.NotEmpty(t, summary.Repositories[0].Reason)
}

func TestPending_ReadsTheManagersRowsAsTheyCome(t *testing.T) {
	// A declaration without a lifecycle: the manager omits the key.
	payload := `{"scope":"mine","repositories":[{"repository":"giantswarm/new-service","team":"team-honeybadger","archived":false,"setup":{},"age":"1h"}]}`
	p := &pendingPass{
		open: func(context.Context) (*client.Session, error) {
			return &client.Session{Caller: rawManager(payload)}, nil
		},
	}

	repos, err := p.pending(t.Context())

	require.NoError(t, err)
	require.Equal(t, []string{"giantswarm/new-service"}, repos)
}

// rawManager answers every call with payload.
type rawManager string

func (m rawManager) Call(context.Context, string, map[string]any) (json.RawMessage, error) {
	return json.RawMessage(m), nil
}

func TestPending_WarnsWhenTheListIsCut(t *testing.T) {
	payload := `{"scope":"mine","matched":130,"shown":100,"repositories":[]}`
	var warnings []string
	p := &pendingPass{
		open: func(context.Context) (*client.Session, error) {
			return &client.Session{Caller: rawManager(payload)}, nil
		},
		warn: func(w string) { warnings = append(warnings, w) },
	}

	_, err := p.pending(t.Context())

	require.NoError(t, err)
	require.Equal(t, []string{"130 repositories of your teams wait for a CircleCI pipeline; this pass reads the first 100"}, warnings)
}

// setupWorkflowsOff is a project set up already, with its setup workflows
// switched off.
type setupWorkflowsOff struct{ *fakeCircleCI }

func (setupWorkflowsOff) GetProjectSettings(context.Context, string, string) (*circleciclient.ProjectSettings, error) {
	off := false
	return &circleciclient.ProjectSettings{Advanced: circleciclient.AdvancedSettings{SetupWorkflows: &off}}, nil
}

func TestPending_ASwitchedSettingIsAChange(t *testing.T) {
	m := &fakeManager{rows: []manager.Row{{Repository: "giantswarm/mcp-template", Team: "team-honeybadger"}}}
	ci := &fakeCircleCI{setUp: true}
	p := newPending(m, ci)
	p.openCI = func(context.Context, string, func(string)) (circleciapp.GitHub, circleciapp.CircleCI, error) {
		return fakeGitHub{}, setupWorkflowsOff{ci}, nil
	}

	summary, err := p.run(t.Context())

	require.NoError(t, err)
	require.Equal(t, OutcomeCreated, summary.Repositories[0].Outcome)
	require.Equal(t, "the pipeline existed; switched the project's setup workflows on", summary.Repositories[0].Reason)
}
