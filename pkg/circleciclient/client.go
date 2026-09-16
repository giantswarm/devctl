// Package circleciclient is devctl's client for the CircleCI API: the calls
// the repository set-up engine makes for a project — follow and unfollow
// (v1.1), the project, its settings and checkout keys, its pipelines and
// their workflows and jobs (v2) — and nothing else. The token is a personal
// API token (architectbot's `CIRCLECI_API_TOKEN` for the reconciler, the
// person's for `devctl repo reconcile`); the org and repository name a
// project by their GitHub slug.
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
)

// DefaultBaseURL is CircleCI's public API host.
const DefaultBaseURL = "https://circleci.com"

// KeyTypeDeployKey is the checkout key type CircleCI creates as a deploy key
// on the GitHub repository; a project without one cannot check out.
const KeyTypeDeployKey = "deploy-key"

// Config configures a Client.
type Config struct {
	// Token is the CircleCI API token, sent as the Circle-Token header.
	Token string
	// BaseURL overrides the API host; empty means [DefaultBaseURL].
	BaseURL string
	// HTTPClient overrides the HTTP client; nil means one with a timeout.
	HTTPClient *http.Client
	// Logger receives one debug line per request; nil discards.
	Logger *logrus.Logger
}

// Client calls the CircleCI API.
type Client struct {
	token   string
	baseURL string
	http    *http.Client
	logger  *logrus.Logger
}

// New returns a Client for config.
func New(config Config) (*Client, error) {
	if config.Token == "" {
		return nil, microerror.Maskf(invalidConfigError, "%T.Token must not be empty", config)
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
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	logger := config.Logger
	if logger == nil {
		logger = logrus.New()
		logger.SetOutput(io.Discard)
	}
	return &Client{
		token:   config.Token,
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    httpClient,
		logger:  logger,
	}, nil
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

// ProjectSettings are the v2 project settings; only the advanced settings
// the engine manages are modelled. Pointer fields are omitted when nil, so a
// value can carry just the settings to change.
type ProjectSettings struct {
	Advanced AdvancedSettings `json:"advanced"`
}

// AdvancedSettings are the settings under "advanced".
type AdvancedSettings struct {
	// SetupWorkflows enables dynamic configuration: the setup workflow the
	// generated pipeline needs (`Use of setup workflows must be enabled in
	// project settings` otherwise).
	SetupWorkflows *bool `json:"setup_workflows,omitempty"`
}

// CheckoutKey is a key CircleCI checks the repository out with.
type CheckoutKey struct {
	Type        string    `json:"type"`
	Preferred   bool      `json:"preferred"`
	Fingerprint string    `json:"fingerprint"`
	PublicKey   string    `json:"public_key"`
	CreatedAt   time.Time `json:"created_at"`
}

// Pipeline is one pipeline of a project.
type Pipeline struct {
	ID        string      `json:"id"`
	Number    int64       `json:"number"`
	State     string      `json:"state"`
	CreatedAt time.Time   `json:"created_at"`
	VCS       PipelineVCS `json:"vcs"`
}

// PipelineVCS is what a pipeline built.
type PipelineVCS struct {
	Tag      string `json:"tag"`
	Branch   string `json:"branch"`
	Revision string `json:"revision"`
}

// TriggerRequest names the revision a pipeline is triggered for: a tag or a
// branch, one of the two.
type TriggerRequest struct {
	Tag    string `json:"tag,omitempty"`
	Branch string `json:"branch,omitempty"`
}

// Workflow is one workflow of a pipeline. Status is one of success,
// running, not_run, failed, error, failing, on_hold, canceled, unauthorized.
type Workflow struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Status         string `json:"status"`
	PipelineNumber int64  `json:"pipeline_number"`
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

// Job is one job of a workflow.
type Job struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Type   string `json:"type"`
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

// Follow follows the GitHub repository org/repo (v1.1). CircleCI builds the
// default branch at once with whatever .circleci/config.yml it carries and
// refuses an empty repository, so the scaffold is pushed first.
func (c *Client) Follow(ctx context.Context, org, repo string) error {
	var out struct {
		Followed bool `json:"followed"`
	}
	if err := c.do(ctx, http.MethodPost, c.v1Project(org, repo)+"/follow", nil, &out); err != nil {
		return microerror.Mask(err)
	}
	if out.Followed {
		return nil
	}
	// The first follow of a fresh project answers followed=false although
	// the project is followed from then on (seen live on a repository
	// created minutes before); the project itself is the truth.
	if _, err := c.GetProject(ctx, org, repo); err != nil {
		if IsNotFound(err) {
			return microerror.Maskf(apiError, "follow %s/%s: CircleCI answered followed=false and knows no such project", org, repo)
		}
		return microerror.Mask(err)
	}
	return nil
}

// Unfollow unfollows org/repo (v1.1): no more builds, the project stays
// readable.
func (c *Client) Unfollow(ctx context.Context, org, repo string) error {
	return microerror.Mask(c.do(ctx, http.MethodPost, c.v1Project(org, repo)+"/unfollow", nil, nil))
}

// GetProjectSettings returns the v2 project settings.
func (c *Client) GetProjectSettings(ctx context.Context, org, repo string) (*ProjectSettings, error) {
	var s ProjectSettings
	if err := c.do(ctx, http.MethodGet, c.v2Project(org, repo)+"/settings", nil, &s); err != nil {
		return nil, microerror.Mask(err)
	}
	return &s, nil
}

// UpdateProjectSettings patches the settings set in settings and returns the
// settings as they are afterwards.
func (c *Client) UpdateProjectSettings(ctx context.Context, org, repo string, settings ProjectSettings) (*ProjectSettings, error) {
	var s ProjectSettings
	if err := c.do(ctx, http.MethodPatch, c.v2Project(org, repo)+"/settings", settings, &s); err != nil {
		return nil, microerror.Mask(err)
	}
	return &s, nil
}

// ListCheckoutKeys returns the project's checkout keys.
func (c *Client) ListCheckoutKeys(ctx context.Context, org, repo string) ([]CheckoutKey, error) {
	var out struct {
		Items []CheckoutKey `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, c.v2Project(org, repo)+"/checkout-key", nil, &out); err != nil {
		return nil, microerror.Mask(err)
	}
	return out.Items, nil
}

// CreateCheckoutKey creates a checkout key of keyType ([KeyTypeDeployKey] or
// user-key); CircleCI installs a deploy key on the GitHub repository.
func (c *Client) CreateCheckoutKey(ctx context.Context, org, repo, keyType string) (*CheckoutKey, error) {
	body := map[string]string{"type": keyType}
	var k CheckoutKey
	if err := c.do(ctx, http.MethodPost, c.v2Project(org, repo)+"/checkout-key", body, &k); err != nil {
		return nil, microerror.Mask(err)
	}
	return &k, nil
}

// ListPipelines returns the project's most recent pipelines (the first page,
// newest first).
func (c *Client) ListPipelines(ctx context.Context, org, repo string) ([]Pipeline, error) {
	var out struct {
		Items []Pipeline `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, c.v2Project(org, repo)+"/pipeline", nil, &out); err != nil {
		return nil, microerror.Mask(err)
	}
	return out.Items, nil
}

// TriggerPipeline triggers a pipeline for the tag or branch in req: the way
// a tag build the project missed (followed after the tag, renamed) is run.
func (c *Client) TriggerPipeline(ctx context.Context, org, repo string, req TriggerRequest) (*Pipeline, error) {
	if (req.Tag == "") == (req.Branch == "") {
		return nil, microerror.Maskf(invalidConfigError, "%T: exactly one of Tag and Branch must be set", req)
	}
	var p Pipeline
	if err := c.do(ctx, http.MethodPost, c.v2Project(org, repo)+"/pipeline", req, &p); err != nil {
		return nil, microerror.Mask(err)
	}
	if req.Tag != "" {
		p.VCS.Tag = req.Tag
	} else {
		p.VCS.Branch = req.Branch
	}
	return &p, nil
}

// ListPipelineWorkflows returns the workflows of a pipeline.
func (c *Client) ListPipelineWorkflows(ctx context.Context, pipelineID string) ([]Workflow, error) {
	var out struct {
		Items []Workflow `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v2/pipeline/"+url.PathEscape(pipelineID)+"/workflow", nil, &out); err != nil {
		return nil, microerror.Mask(err)
	}
	return out.Items, nil
}

// ListWorkflowJobs returns the jobs of a workflow.
func (c *Client) ListWorkflowJobs(ctx context.Context, workflowID string) ([]Job, error) {
	var out struct {
		Items []Job `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v2/workflow/"+url.PathEscape(workflowID)+"/job", nil, &out); err != nil {
		return nil, microerror.Mask(err)
	}
	return out.Items, nil
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
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
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
	req.Header.Set("Circle-Token", c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.logger.Debugf("circleci: %s %s", method, path)

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
	case resp.StatusCode == http.StatusUnauthorized:
		// v1.1 says "Invalid token provided", v2 "New format tokens are
		// needed": both also describe a token read with its quotes.
		return microerror.Maskf(apiError, "%s %s: HTTP 401 %s (check CIRCLECI_API_TOKEN: a token read with its surrounding quotes fails the same way)", method, path, apiMessage(data))
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
