package circleciclient

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/giantswarm/microerror"
)

// APIv2Suffix is the path circleci.com serves the API v2 under. The
// agent-facing commands are configured with the API URL including it
// (https://circleci.com/api/v2); [BaseURLFromAPIURL] turns that into the
// host this client prefixes its paths to.
const APIv2Suffix = "/api/v2"

// BaseURLFromAPIURL returns the Config.BaseURL for an API v2 URL: the URL
// without its /api/v2 suffix.
func BaseURLFromAPIURL(apiURL string) string {
	return strings.TrimSuffix(strings.TrimRight(apiURL, "/"), APIv2Suffix)
}

// NewestWorkflows keeps the newest run of every workflow name, sorted by
// name: a rerun (from failed or in full) is a second workflow of the same
// name in the same pipeline, and the one it replaces keeps its failed status
// for ever, so only the newest run of a name says where the pipeline stands.
func NewestWorkflows(runs []Workflow) []Workflow {
	newest := map[string]Workflow{}
	for _, run := range runs {
		current, ok := newest[run.Name]
		if !ok || run.CreatedAt.After(current.CreatedAt) {
			newest[run.Name] = run
		}
	}
	out := make([]Workflow, 0, len(newest))
	for _, run := range newest {
		out = append(out, run)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// WorkflowRunning says whether a workflow status is still moving.
func WorkflowRunning(status string) bool {
	switch status {
	case "running", "on_hold", "failing":
		return true
	}
	return false
}

// tagPipelinePages bounds the pipeline listing of FindPipelineByTag: the
// pipeline of a fresh tag is among the newest.
const tagPipelinePages = 3

// FindPipelineByTag returns the newest pipeline of org/repo that built tag,
// or nil when the newest pipelines do not include one: the tag's webhook
// has not reached CircleCI yet, or never will.
func (c *Client) FindPipelineByTag(ctx context.Context, org, repo, tag string) (*Pipeline, error) {
	token := ""
	for page := 0; page < tagPipelinePages; page++ {
		pipelines, err := c.ListPipelines(ctx, org, repo, token)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		for i := range pipelines.Items {
			if pipelines.Items[i].VCS.Tag == tag {
				return &pipelines.Items[i], nil
			}
		}
		if pipelines.NextPageToken == "" {
			break
		}
		token = pipelines.NextPageToken
	}
	return nil, nil
}

// PipelineURL is the pipeline's page in the CircleCI UI.
func PipelineURL(org, repo string, number int64) string {
	return fmt.Sprintf("https://app.circleci.com/pipelines/github/%s/%s/%d", org, repo, number)
}
