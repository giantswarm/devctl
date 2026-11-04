// Package circleciclient is devctl's client for the CircleCI API: the token's
// user, the project, its pipeline definitions and their triggers, a
// pipeline run of a definition, its pipelines (paged) and their workflows
// and jobs (v2), a job's steps and their output (v1.1), a workflow's cancel
// and rerun (v2) — and nothing else. The token is the person's: their devctl
// CircleCI login. CircleCI has no service accounts, and the reconciler holds
// no CircleCI token; the org and repository name a project by their GitHub
// slug. A reader without a token of its own reads a
// public project's pipelines, workflows and jobs anonymously
// ([Config.Anonymous]): CircleCI answers those reads for a public project
// without one, and a private project with 404.
package circleciclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// DefaultBaseURL is CircleCI's public API host.
const DefaultBaseURL = "https://circleci.com"

// Config configures a Client.
type Config struct {
	// Token is the CircleCI API token, sent as the Circle-Token header.
	Token string
	// TokenSource names where Token came from for the 401 hint: an
	// environment variable ("$CIRCLECI_TOKEN"), or a description such as the
	// devctl keychain. Empty means "the CircleCI token".
	TokenSource string
	// Renew re-reads the selected token source after a read gets 401. When
	// set, the read is retried once after two seconds, even if the token is
	// unchanged. Nil preserves a fixed credential, without retrying 401.
	Renew func(ctx context.Context, rejected string) (string, error)
	// Anonymous is a client without a token: it reads what CircleCI answers
	// without one, the pipelines, workflows and jobs of a public project (a
	// private one answers 404, IsNotFound), and no header is sent. Set with
	// an empty Token; a token and Anonymous together are a config error, as
	// is neither, so a reader meant to hold a token does not read anonymously
	// by accident.
	Anonymous bool
	// BaseURL overrides the API host; empty means [DefaultBaseURL].
	BaseURL string
	// HTTPClient overrides the HTTP client; nil means one with a timeout,
	// sending through Transport.
	HTTPClient *http.Client
	// Transport sends the requests of the default HTTP client; nil means
	// http.DefaultTransport. A caller counting the requests builds its
	// counter here.
	Transport http.RoundTripper
	// Logger receives one debug line per request; nil discards.
	Logger *logrus.Logger
}

// Client calls the CircleCI API.
type Client struct {
	token            string
	unauthorizedHint string
	baseURL          string
	http             *http.Client
	logger           *logrus.Logger
}

// New returns a Client for config.
func New(config Config) (*Client, error) {
	switch {
	case config.Token == "" && !config.Anonymous:
		return nil, microerror.Maskf(invalidConfigError, "%T.Token must not be empty", config)
	case config.Token != "" && config.Anonymous:
		return nil, microerror.Maskf(invalidConfigError, "%T.Anonymous takes no Token", config)
	}
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if _, err := url.Parse(baseURL); err != nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.BaseURL: %v", config, err)
	}
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second, Transport: config.Transport}
	}
	if config.Renew != nil {
		copy := *httpClient
		base := copy.Transport
		if base == nil {
			base = http.DefaultTransport
		}
		copy.Transport = &reauthorizing{base: base, renew: config.Renew, token: config.Token}
		httpClient = &copy
	}
	logger := config.Logger
	if logger == nil {
		logger = logrus.New()
		logger.SetOutput(io.Discard)
	}
	return &Client{
		token:            config.Token,
		unauthorizedHint: unauthorizedHint(config.TokenSource),
		baseURL:          strings.TrimRight(baseURL, "/"),
		http:             httpClient,
		logger:           logger,
	}, nil
}

// unauthorizedHint is what a 401 asks to check: the token's source, and for
// a variable the quotes a token is easily read with, which fail the same way.
func unauthorizedHint(source string) string {
	switch {
	case source == "":
		return "check the CircleCI token"
	case strings.HasPrefix(source, "$"):
		return "check " + source + ": a token read with its surrounding quotes fails the same way"
	default:
		return "check the token from " + source
	}
}

// Project is a CircleCI project as the v2 API describes it.
type Project struct {
	Slug             string  `json:"slug"`
	Name             string  `json:"name"`
	ID               string  `json:"id"`
	OrganizationName string  `json:"organization_name"`
	VCSInfo          VCSInfo `json:"vcs_info"`
}

// VCSInfo is the repository a project builds.
type VCSInfo struct {
	VCSURL        string `json:"vcs_url"`
	Provider      string `json:"provider"`
	DefaultBranch string `json:"default_branch"`
}

// Pipeline is one pipeline of a project.
type Pipeline struct {
	ID        string          `json:"id"`
	Number    int64           `json:"number"`
	State     string          `json:"state"`
	CreatedAt time.Time       `json:"created_at"`
	VCS       PipelineVCS     `json:"vcs"`
	Trigger   PipelineTrigger `json:"trigger"`
}

// UnmarshalJSON reads a pipeline. One the CircleCI GitHub App triggered
// carries no vcs: when vcs is absent, what it built is read from
// trigger_parameters.git, the checkout's revision, its branch (or the branch
// of a ref under refs/heads/) and the tag of a ref under refs/tags/.
func (p *Pipeline) UnmarshalJSON(data []byte) error {
	type pipeline Pipeline
	var raw struct {
		pipeline
		TriggerParameters struct {
			Git json.RawMessage `json:"git"`
		} `json:"trigger_parameters"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*p = Pipeline(raw.pipeline)
	if p.VCS == (PipelineVCS{}) {
		p.VCS = triggerVCS(raw.TriggerParameters.Git)
	}
	return nil
}

// triggerVCS reads what a pipeline built from its trigger_parameters.git, or
// nothing when that is absent or not the shape the GitHub App gives it: an
// API-triggered pipeline's parameters are the project's own, and one named
// git must not fail the read of the pipeline.
func triggerVCS(data json.RawMessage) PipelineVCS {
	var git struct {
		CheckoutSHA string `json:"checkout_sha"`
		Branch      string `json:"branch"`
		Ref         string `json:"ref"`
	}
	if len(data) == 0 || json.Unmarshal(data, &git) != nil || git.CheckoutSHA == "" {
		return PipelineVCS{}
	}
	if tag, ok := strings.CutPrefix(git.Ref, "refs/tags/"); ok {
		return PipelineVCS{Revision: git.CheckoutSHA, Tag: tag}
	}
	branch := git.Branch
	if branch == "" {
		branch, _ = strings.CutPrefix(git.Ref, "refs/heads/")
	}
	return PipelineVCS{Revision: git.CheckoutSHA, Branch: branch}
}

// PipelineTrigger is what created a pipeline. Type is webhook for a push,
// api (or explicit) for a pipeline triggered through the API, schedule or
// scheduled_pipeline for a schedule.
type PipelineTrigger struct {
	Type string `json:"type"`
}

// TriggeredByAPI says whether the pipeline was triggered through the API,
// as [Client.TriggerTagPipeline] does, rather than by a push.
func (p Pipeline) TriggeredByAPI() bool {
	return p.Trigger.Type == "api" || p.Trigger.Type == "explicit"
}

// PipelineVCS is what a pipeline built.
type PipelineVCS struct {
	Tag      string `json:"tag"`
	Branch   string `json:"branch"`
	Revision string `json:"revision"`
}

// Workflow is one workflow of a pipeline. Status is one of success,
// running, not_run, failed, error, failing, on_hold, canceled, unauthorized.
type Workflow struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	// StoppedAt is when the workflow finished; zero while it runs.
	StoppedAt      time.Time `json:"stopped_at"`
	PipelineNumber int64     `json:"pipeline_number"`
	// CreatedAt orders the runs of one workflow name: a rerun is a new
	// workflow with the same name in the same pipeline, and the newest counts.
	CreatedAt time.Time `json:"created_at"`
	// Tag is [WorkflowTagSetup] for the setup workflow of a dynamic-config
	// pipeline, the one that continues the pipeline with the workflows that
	// carry the build; empty for every other workflow.
	Tag string `json:"tag"`
}

// WorkflowTagSetup is the Workflow.Tag of a setup workflow.
const WorkflowTagSetup = "setup"

// SetupOnly says whether workflows are a dynamic-config pipeline's setup
// workflow and nothing else: the continuation has not created the build's
// workflows yet, or CircleCI's listing lags behind them.
func SetupOnly(workflows []Workflow) bool {
	if len(workflows) == 0 {
		return false
	}
	for _, w := range workflows {
		if w.Tag != WorkflowTagSetup {
			return false
		}
	}
	return true
}

// PipelineContinuing says whether a pipeline state is one of a setup
// pipeline whose configuration is not continued yet: its setup workflow
// queued or running (setup-pending, setup), or its continuation submitted and
// not created (pending). The workflows that carry the build do not exist yet;
// a settled pipeline reads created (or errored).
func PipelineContinuing(state string) bool {
	switch state {
	case "setup-pending", "setup", "pending":
		return true
	}
	return false
}

// WorkflowSucceeded says whether a workflow status is a finished success.
func WorkflowSucceeded(status string) bool { return status == "success" }

// WorkflowFailed says whether a workflow status is a terminal failure: the
// tag it built is dead and the fix lands as the next tag.
func WorkflowFailed(status string) bool {
	switch status {
	case "failed", "error", "failing", "canceled", "unauthorized":
		return true
	}
	return false
}

// WorkflowFinished says whether a workflow status is final: a success or a
// terminal failure. A workflow that is not finished can still change, and so
// can what CircleCI answers about it -- its jobs are listed only once it has
// set them up.
func WorkflowFinished(status string) bool { return WorkflowSucceeded(status) || WorkflowFailed(status) }

// Job is one job of a workflow. JobNumber is absent for an approval and for
// a job that has not started, and so are StartedAt (zero) and, while the job
// runs, StoppedAt.
type Job struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	Type      string    `json:"type"`
	JobNumber int64     `json:"job_number"`
	StartedAt time.Time `json:"started_at"`
	StoppedAt time.Time `json:"stopped_at"`
}

// JobFailed says whether a job status is a terminal failure.
func JobFailed(status string) bool {
	switch status {
	case "failed", "error", "canceled", "timedout", "infrastructure_fail", "unauthorized":
		return true
	}
	return false
}

// User is the token's CircleCI user (GET /api/v2/me). For an account
// connected through GitHub, Login is the GitHub login: the user CircleCI
// follows a project as.
type User struct {
	ID    string `json:"id"`
	Login string `json:"login"`
	Name  string `json:"name"`
}

// Me returns the token's user.
func (c *Client) Me(ctx context.Context) (*User, error) {
	var u User
	if err := c.do(ctx, http.MethodGet, "/api/v2/me", nil, &u); err != nil {
		return nil, microerror.Mask(err)
	}
	return &u, nil
}

// GetProject returns the project of org/repo; IsNotFound when CircleCI does
// not know it, which for a repository on GitHub means it is not followed.
func (c *Client) GetProject(ctx context.Context, org, repo string) (*Project, error) {
	var p Project
	if err := c.do(ctx, http.MethodGet, c.v2Project(org, repo), nil, &p); err != nil {
		return nil, microerror.Mask(err)
	}
	return &p, nil
}

// PipelinePage is one page of a project's pipelines, newest first, and the
// token of the page after it — empty on the last page.
type PipelinePage struct {
	Items         []Pipeline `json:"items"`
	NextPageToken string     `json:"next_page_token"`
}

// ListPipelines returns one page of the project's pipelines, newest first:
// the most recent ones for an empty pageToken, the page after a page for its
// NextPageToken.
func (c *Client) ListPipelines(ctx context.Context, org, repo, pageToken string) (*PipelinePage, error) {
	path := c.v2Project(org, repo) + "/pipeline"
	if pageToken != "" {
		path += "?page-token=" + url.QueryEscape(pageToken)
	}
	var page PipelinePage
	if err := c.do(ctx, http.MethodGet, path, nil, &page); err != nil {
		return nil, microerror.Mask(err)
	}
	return &page, nil
}

// TriggerTagPipeline runs the project's pipeline for tag: the build of a tag
// pushed before CircleCI followed the project, which CircleCI never saw.
func (c *Client) TriggerTagPipeline(ctx context.Context, org, repo, tag string) (*Pipeline, error) {
	if tag == "" {
		return nil, microerror.Maskf(invalidConfigError, "tag must not be empty")
	}
	var p Pipeline
	if err := c.do(ctx, http.MethodPost, c.v2Project(org, repo)+"/pipeline", map[string]string{"tag": tag}, &p); err != nil {
		return nil, microerror.Mask(err)
	}
	p.VCS.Tag = tag
	return &p, nil
}

// ListPipelineWorkflows returns the workflows of a pipeline.
func (c *Client) ListPipelineWorkflows(ctx context.Context, pipelineID string) ([]Workflow, error) {
	var out []Workflow
	base := "/api/v2/pipeline/" + url.PathEscape(pipelineID) + "/workflow"
	path := base
	for {
		var page struct {
			Items         []Workflow `json:"items"`
			NextPageToken string     `json:"next_page_token"`
		}
		if err := c.do(ctx, http.MethodGet, path, nil, &page); err != nil {
			return nil, microerror.Mask(err)
		}
		out = append(out, page.Items...)
		if page.NextPageToken == "" {
			return out, nil
		}
		path = base + "?page-token=" + url.QueryEscape(page.NextPageToken)
	}
}

// ListWorkflowJobs returns every page of the jobs of a workflow, retrying an authenticated
// 404 anonymously for workflows readable through the public API.
func (c *Client) ListWorkflowJobs(ctx context.Context, workflowID string) ([]Job, error) {
	var out []Job
	base := "/api/v2/workflow/" + url.PathEscape(workflowID) + "/job"
	path := base
	for {
		var page struct {
			Items         []Job  `json:"items"`
			NextPageToken string `json:"next_page_token"`
		}
		if err := c.readPublicJob(ctx, path, &page); err != nil {
			return nil, microerror.Mask(err)
		}
		out = append(out, page.Items...)
		if page.NextPageToken == "" {
			return out, nil
		}
		path = base + "?page-token=" + url.QueryEscape(page.NextPageToken)
	}
}

// RerunWorkflowFromFailed reruns the failed jobs of a finished workflow and
// the jobs that depend on them, and returns the id of the new workflow: the
// rerun is a second workflow of the same name in the same pipeline. A token
// without write access is refused with IsForbidden.
func (c *Client) RerunWorkflowFromFailed(ctx context.Context, workflowID string) (string, error) {
	id, err := c.RerunWorkflow(ctx, workflowID, true)
	return id, microerror.Mask(err)
}

// FailedStepsOutput returns the output of the failed steps of job number of
// org/repo (v1.1: API v2 has no job output), in step order. The output
// lives at a signed URL per step, read without the token. An authenticated
// 404 reading the job is retried anonymously for public jobs.
func (c *Client) FailedStepsOutput(ctx context.Context, org, repo string, number int64) (string, error) {
	var job struct {
		Steps []struct {
			Actions []struct {
				Failed    bool   `json:"failed"`
				OutputURL string `json:"output_url"`
			} `json:"actions"`
		} `json:"steps"`
	}
	if err := c.readPublicJob(ctx, fmt.Sprintf("%s/%d", c.v1Project(org, repo), number), &job); err != nil {
		return "", microerror.Mask(err)
	}
	var out strings.Builder
	for _, step := range job.Steps {
		for _, action := range step.Actions {
			if !action.Failed || action.OutputURL == "" {
				continue
			}
			if err := c.stepOutput(ctx, action.OutputURL, &out); err != nil {
				return "", microerror.Mask(err)
			}
		}
	}
	return out.String(), nil
}

// readPublicJob retries a job read once without the token when CircleCI
// hides it from the authenticated caller with 404 but may expose it publicly.
// Explicit authentication refusals are returned as-is. The copy keeps the
// original client's identity, transport and timeout intact for other calls.
func (c *Client) readPublicJob(ctx context.Context, path string, out any) error {
	err := c.do(ctx, http.MethodGet, path, nil, out)
	if IsNotFound(err) && c.token != "" {
		anonymous := *c
		anonymous.token = ""
		err = anonymous.do(ctx, http.MethodGet, path, nil, out)
	}
	return microerror.Mask(err)
}

// stepOutput appends the messages of one step's output to out.
func (c *Client) stepOutput(ctx context.Context, outputURL string, out *strings.Builder) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, outputURL, nil)
	if err != nil {
		return microerror.Mask(err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return microerror.Mask(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return microerror.Maskf(apiError, "step output: HTTP %d", resp.StatusCode)
	}
	var messages []struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
		return microerror.Maskf(apiError, "step output: invalid JSON answer: %v", err)
	}
	for _, m := range messages {
		out.WriteString(m.Message)
	}
	return nil
}

func (c *Client) v1Project(org, repo string) string {
	return fmt.Sprintf("/api/v1.1/project/github/%s/%s", url.PathEscape(org), url.PathEscape(repo))
}

func (c *Client) v2Project(org, repo string) string {
	return fmt.Sprintf("/api/v2/project/gh/%s/%s", url.PathEscape(org), url.PathEscape(repo))
}

// do sends one request with body (JSON-encoded when not nil) and decodes a
// 2xx answer into out (when not nil). A 404 is IsNotFound; every other
// non-2xx status is an apiError carrying CircleCI's message.
func (c *Client) do(ctx context.Context, method, path string, body, out any) (err error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return microerror.Mask(err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return microerror.Mask(err)
	}
	if c.token != "" {
		req.Header.Set("Circle-Token", c.token)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.logger.Debugf("circleci: %s %s", method, path)

	defer func() {
		if err != nil {
			err = &agentcli.RemoteError{Err: err}
		}
	}()
	resp, err := c.http.Do(req)
	if err != nil {
		return microerror.Mask(err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return microerror.Mask(err)
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return microerror.Maskf(notFoundError, "%s %s: %s", method, path, apiMessage(data))
	case resp.StatusCode == http.StatusForbidden:
		return microerror.Maskf(forbiddenError, "%s %s: HTTP 403 %s", method, path, apiMessage(data))
	case resp.StatusCode == http.StatusUnauthorized:
		// v1.1 says "Invalid token provided", v2 "New format tokens are
		// needed": both also describe a token read with its quotes.
		return microerror.Maskf(apiError, "%s %s: HTTP 401 %s (%s)", method, path, apiMessage(data), c.unauthorizedHint)
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return microerror.Maskf(apiError, "%s %s: HTTP %d %s", method, path, resp.StatusCode, apiMessage(data))
	}
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return microerror.Maskf(apiError, "%s %s: invalid JSON answer: %v", method, path, err)
	}
	return nil
}

// apiMessage extracts CircleCI's error message from an answer body.
func apiMessage(data []byte) string {
	var m struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &m) == nil && m.Message != "" {
		return m.Message
	}
	s := strings.TrimSpace(string(data))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
