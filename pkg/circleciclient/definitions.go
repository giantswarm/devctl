package circleciclient

import (
	"context"
	"net/http"
	"net/url"

	"github.com/giantswarm/microerror"
)

// The providers of a pipeline definition's config, checkout and trigger
// event sources: the CircleCI GitHub App, or the OAuth integration of a
// project followed by a person.
const (
	ProviderGitHubApp   = "github_app"
	ProviderGitHubOAuth = "github_oauth"
)

// EventPresetAllPushes is the trigger preset that builds every push of a
// branch or a tag.
const EventPresetAllPushes = "all-pushes"

// ConfigFile is the pipeline configuration a definition reads.
const ConfigFile = ".circleci/config.yml"

// DefinitionRepo is the GitHub repository of a definition's source.
// ExternalID is GitHub's repository id; CircleCI fills FullName in.
type DefinitionRepo struct {
	FullName   string `json:"full_name,omitempty"`
	ExternalID string `json:"external_id"`
}

// DefinitionSource is a definition's config or checkout source, or a
// trigger's event source. FilePath is set on the config source only.
type DefinitionSource struct {
	Provider string         `json:"provider"`
	Repo     DefinitionRepo `json:"repo"`
	FilePath string         `json:"file_path,omitempty"`
}

// PipelineDefinition is one pipeline of a project: where its configuration
// and checkout come from. An OAuth project carries one implicit definition
// of provider github_oauth; a GitHub App pipeline is a definition of
// provider github_app beside it.
type PipelineDefinition struct {
	ID             string           `json:"id,omitempty"`
	Name           string           `json:"name"`
	Description    string           `json:"description,omitempty"`
	ConfigSource   DefinitionSource `json:"config_source"`
	CheckoutSource DefinitionSource `json:"checkout_source"`
}

// GitHubApp says whether the definition builds through the CircleCI GitHub
// App: its configuration is read by the App's installation, so whoever
// created it is not load-bearing.
func (d PipelineDefinition) GitHubApp() bool {
	return d.ConfigSource.Provider == ProviderGitHubApp
}

// Trigger is what starts a definition's pipelines: for the GitHub App, a
// preset of the repository's events.
type Trigger struct {
	ID          string           `json:"id,omitempty"`
	EventSource DefinitionSource `json:"event_source"`
	EventPreset string           `json:"event_preset,omitempty"`
	Disabled    bool             `json:"disabled,omitempty"`
}

// AppDefinition is a GitHub App definition of repoID with config file
// ConfigFile, named name.
func AppDefinition(name, description, repoID string) PipelineDefinition {
	source := DefinitionSource{Provider: ProviderGitHubApp, Repo: DefinitionRepo{ExternalID: repoID}}
	config := source
	config.FilePath = ConfigFile
	return PipelineDefinition{Name: name, Description: description, ConfigSource: config, CheckoutSource: source}
}

// AllPushesTrigger is the trigger of a GitHub App definition of repoID that
// builds every branch and tag push.
func AllPushesTrigger(repoID string) Trigger {
	return Trigger{
		EventSource: DefinitionSource{Provider: ProviderGitHubApp, Repo: DefinitionRepo{ExternalID: repoID}},
		EventPreset: EventPresetAllPushes,
	}
}

// ListPipelineDefinitions returns every definition of the project with id
// projectID ([Project.ID]).
func (c *Client) ListPipelineDefinitions(ctx context.Context, projectID string) ([]PipelineDefinition, error) {
	var out []PipelineDefinition
	path := c.definitions(projectID)
	for {
		var page struct {
			Items         []PipelineDefinition `json:"items"`
			NextPageToken string               `json:"next_page_token"`
		}
		if err := c.do(ctx, http.MethodGet, path, nil, &page); err != nil {
			return nil, microerror.Mask(err)
		}
		out = append(out, page.Items...)
		if page.NextPageToken == "" || len(page.Items) == 0 {
			return out, nil
		}
		path = c.definitions(projectID) + "?page-token=" + url.QueryEscape(page.NextPageToken)
	}
}

// CreatePipelineDefinition creates def on the project and returns it as
// CircleCI stored it, with its id. A login that granted Read access only is
// refused with IsForbidden.
func (c *Client) CreatePipelineDefinition(ctx context.Context, projectID string, def PipelineDefinition) (*PipelineDefinition, error) {
	var out PipelineDefinition
	if err := c.do(ctx, http.MethodPost, c.definitions(projectID), def, &out); err != nil {
		return nil, microerror.Mask(err)
	}
	return &out, nil
}

// ListTriggers returns the triggers of a definition.
func (c *Client) ListTriggers(ctx context.Context, projectID, definitionID string) ([]Trigger, error) {
	var out struct {
		Items []Trigger `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, c.definitions(projectID)+"/"+url.PathEscape(definitionID)+"/triggers", nil, &out); err != nil {
		return nil, microerror.Mask(err)
	}
	return out.Items, nil
}

// CreateTrigger adds trigger to a definition and returns it with its id.
func (c *Client) CreateTrigger(ctx context.Context, projectID, definitionID string, trigger Trigger) (*Trigger, error) {
	var out Trigger
	if err := c.do(ctx, http.MethodPost, c.definitions(projectID)+"/"+url.PathEscape(definitionID)+"/triggers", trigger, &out); err != nil {
		return nil, microerror.Mask(err)
	}
	return &out, nil
}

// DeleteTrigger deletes a trigger. Triggers are addressed under the project,
// not under their definition: CircleCI answers the definition's path with
// 404 "Route Not Found".
func (c *Client) DeleteTrigger(ctx context.Context, projectID, triggerID string) error {
	return microerror.Mask(c.do(ctx, http.MethodDelete, "/api/v2/projects/"+url.PathEscape(projectID)+"/triggers/"+url.PathEscape(triggerID), nil, nil))
}

// Ref is a branch or a tag to run a pipeline on; exactly one is set.
type Ref struct {
	Branch string `json:"branch,omitempty"`
	Tag    string `json:"tag,omitempty"`
}

// String is the ref as a person names it.
func (r Ref) String() string {
	if r.Tag != "" {
		return "tag " + r.Tag
	}
	return "branch " + r.Branch
}

// RunPipeline runs the pipeline of a definition of org/repo on ref, its
// configuration and checkout both read from ref. A GitHub App pipeline has
// no other trigger through the API: the v2 trigger of TriggerTagPipeline
// runs the implicit OAuth definition.
func (c *Client) RunPipeline(ctx context.Context, org, repo, definitionID string, ref Ref) (*Pipeline, error) {
	if definitionID == "" || (ref.Branch == "") == (ref.Tag == "") {
		return nil, microerror.Maskf(invalidConfigError, "a definition and exactly one of branch and tag are required")
	}
	body := struct {
		DefinitionID string `json:"definition_id"`
		Config       Ref    `json:"config"`
		Checkout     Ref    `json:"checkout"`
	}{definitionID, ref, ref}
	var p Pipeline
	if err := c.do(ctx, http.MethodPost, c.v2Project(org, repo)+"/pipeline/run", body, &p); err != nil {
		return nil, microerror.Mask(err)
	}
	p.VCS.Branch, p.VCS.Tag = ref.Branch, ref.Tag
	return &p, nil
}

func (c *Client) definitions(projectID string) string {
	return "/api/v2/projects/" + url.PathEscape(projectID) + "/pipeline-definitions"
}

// ProjectSettings are the v2 project settings; only the advanced settings
// devctl manages are modelled. Pointer fields are omitted when nil, so a
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
