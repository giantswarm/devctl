package followcircleci

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

func newPendingRunner(m *fakeManager, ci *fakeCircleCI, output string, out *bytes.Buffer) *runner {
	r := newRunner(ci, out)
	r.flag.Pending = true
	r.flag.Output = output
	r.openSession = func(context.Context) (*client.Session, error) { return &client.Session{Caller: m}, nil }
	return r
}

func TestRunPending_PrintsOneLinePerRepository(t *testing.T) {
	m := &fakeManager{rows: []manager.Row{
		{Repository: "giantswarm/new-service", Team: "team-honeybadger", Lifecycle: "active"},
	}}
	var out bytes.Buffer

	err := newPendingRunner(m, &fakeCircleCI{}, client.OutputText, &out).runPending(t.Context())

	require.NoError(t, err)
	require.Contains(t, out.String(), "giantswarm/new-service: created the GitHub App pipeline, ran branch main (pipeline 1)")
}

func TestRunPending_NothingPending(t *testing.T) {
	var out bytes.Buffer

	err := newPendingRunner(&fakeManager{}, &fakeCircleCI{}, client.OutputText, &out).runPending(t.Context())

	require.NoError(t, err)
	require.Contains(t, out.String(), "no repository of your teams waits for a CircleCI pipeline")
}

// document is follow-circleci --pending's JSON document as an agent reads it.
type document struct {
	agentcli.Envelope
	Summary
}

func TestRunPending_JSONDocument(t *testing.T) {
	m := &fakeManager{rows: []manager.Row{
		{Repository: "giantswarm/new-service", Team: "team-honeybadger", Lifecycle: "active"},
	}}
	var out bytes.Buffer

	err := newPendingRunner(m, &fakeCircleCI{}, client.OutputJSON, &out).runPending(t.Context())

	require.NoError(t, err)
	var doc document
	require.NoError(t, json.Unmarshal(out.Bytes(), &doc))
	require.Equal(t, "repo follow-circleci", doc.Command)
	require.Equal(t, agentcli.VerdictGreen, doc.Verdict)
	require.Equal(t, 1, doc.Pending)
	require.Equal(t, []Entry{{Repository: "giantswarm/new-service", Outcome: OutcomeCreated, Runs: []RunRef{{Ref: "branch main", Pipeline: 1}}}}, doc.Repositories)
}

func TestRunPending_JSONDocumentOfARefusedWrite(t *testing.T) {
	m := &fakeManager{rows: []manager.Row{
		{Repository: "giantswarm/new-service", Team: "team-honeybadger", Lifecycle: "active"},
	}}
	var out bytes.Buffer

	err := newPendingRunner(m, &fakeCircleCI{refuse: http.StatusForbidden}, client.OutputJSON, &out).runPending(t.Context())

	var exitErr *agentcli.ExitError
	require.ErrorAs(t, err, &exitErr)
	require.Equal(t, agentcli.ExitAuthRequired, exitErr.Code)
	var doc document
	require.NoError(t, json.Unmarshal(out.Bytes(), &doc))
	require.Equal(t, agentcli.VerdictAuthRequired, doc.Verdict)
	require.Contains(t, doc.Reason, "grant Write access")
	require.Equal(t, []Outcome{OutcomeRefused}, outcomes(&doc.Summary))
}

func TestRun_PendingTakesNoRepository(t *testing.T) {
	r := newPendingRunner(&fakeManager{}, &fakeCircleCI{}, client.OutputText, &bytes.Buffer{})
	require.Error(t, r.validate([]string{"giantswarm/new-service"}))
	r.flag.Pending = false
	require.Error(t, r.validate(nil))
	r.flag.Output = client.OutputJSON
	require.Error(t, r.validate([]string{"giantswarm/new-service"}), "JSON is --pending's")
	r.flag.Pending, r.flag.Output, r.flag.Migrate = true, client.OutputText, true
	require.Error(t, r.validate(nil), "--migrate is one repository's")
}

func TestRunPending_WithoutAMusterLoginPrintsOnlyTheError(t *testing.T) {
	var out bytes.Buffer
	r := newPendingRunner(&fakeManager{}, &fakeCircleCI{}, client.OutputText, &out)
	r.openSession = func(context.Context) (*client.Session, error) {
		return nil, &authstore.AuthRequiredError{Identity: "muster", Cause: "no token", Hint: "devctl auth login --muster-only"}
	}

	err := r.runPending(t.Context())

	var authErr *authstore.AuthRequiredError
	require.ErrorAs(t, err, &authErr)
	require.Empty(t, out.String())
}
