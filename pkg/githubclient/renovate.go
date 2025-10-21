package githubclient

import (
	"context"
	"fmt"
	"net/http"

	"github.com/giantswarm/microerror"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-github/v76/github"
)

const (
	// Installation ID of Renovate in our organization. Corresponds to
	// https://github.com/organizations/giantswarm/settings/installations/17164699
	renovateInstallationID = 17164699

	renovateIntegrationID = int64(2740)

	githubApiVersion = "2022-11-28"

	rulesetName = "renovate-automerge"
)

var (
	desiredRuleset = github.RepositoryRuleset{
		Name:        rulesetName,
		Target:      github.Ptr(github.RulesetTargetBranch),
		Enforcement: github.RulesetEnforcementActive,
		BypassActors: []*github.BypassActor{
			&github.BypassActor{
				ActorID:    github.Ptr(renovateIntegrationID),
				ActorType:  github.Ptr(github.BypassActorTypeIntegration),
				BypassMode: github.Ptr(github.BypassModePullRequest),
			},
		},
		Conditions: &github.RepositoryRulesetConditions{
			RefName: &github.RepositoryRulesetRefConditionParameters{
				Include: []string{"~DEFAULT_BRANCH"},
				Exclude: []string{},
			},
		},
		CurrentUserCanBypass: github.Ptr(github.BypassModeNever),
		Rules: &github.RepositoryRulesetRules{
			PullRequest: &github.PullRequestRuleParameters{
				AllowedMergeMethods: []github.PullRequestMergeMethod{
					github.PullRequestMergeMethodMerge,
					github.PullRequestMergeMethodSquash,
					github.PullRequestMergeMethodRebase,
				},
				AutomaticCopilotCodeReviewEnabled: github.Ptr(false),
				RequiredApprovingReviewCount:      1,
			},
		},
		SourceType: github.Ptr(github.RulesetSourceTypeRepository),
	}
)

// Add repository to the Renovate installation. Corresponds to
// https://docs.github.com/en/rest/apps/installations?apiVersion=2022-11-28#add-a-repository-to-an-app-installation
func (c *Client) AddRepoToRenovatePermissions(ctx context.Context, org string, repo *github.Repository) error {
	path := fmt.Sprintf("/user/installations/%d/repositories/%d", renovateInstallationID, repo.GetID())
	realClient := c.GetUnderlyingClient(ctx)

	req, err := realClient.NewRequest(http.MethodPut, path, nil, github.WithVersion(githubApiVersion))
	if err != nil {
		return err
	}

	resp, err := realClient.Do(ctx, req, nil)
	if err != nil {
		c.logger.Printf("response: %v", resp)
		return err
	}

	c.logger.Printf("response status: %q", resp.Status)

	return nil
}

// Remove repository from the Renovate installation. Corresponds to
// https://docs.github.com/en/rest/apps/installations?apiVersion=2022-11-28#remove-a-repository-from-an-app-installation
func (c *Client) RemoveRepoFromRenovatePermissions(ctx context.Context, org string, repo *github.Repository) error {
	path := fmt.Sprintf("/user/installations/%d/repositories/%d", renovateInstallationID, repo.GetID())
	realClient := c.GetUnderlyingClient(ctx)

	req, err := realClient.NewRequest(http.MethodDelete, path, nil, github.WithVersion(githubApiVersion))
	if err != nil {
		return err
	}

	resp, err := realClient.Do(ctx, req, nil)
	if err != nil {
		c.logger.Printf("response: %v", resp)
		return err
	}

	c.logger.Printf("response status: %q", resp.Status)

	return nil
}

// ReadRenovateRuleset reads and returns a ruleset by name from the given repository.
func (c *Client) ReadRenovateRuleset(ctx context.Context, owner, repo string) (*github.RepositoryRuleset, error) {
	c.logger.Infof("reading ruleset %s from repository %s/%s", rulesetName, owner, repo)

	underlyingClient := c.GetUnderlyingClient(ctx)

	// List all rulesets in the repository
	rulesets, _, err := underlyingClient.Repositories.GetAllRulesets(ctx, owner, repo, nil)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	// Find the ruleset with the matching name
	id := int64(0)
	for _, ruleset := range rulesets {
		if ruleset.Name == rulesetName {
			c.logger.Debugf("found ruleset %d with name %s in repository %s/%s", ruleset.GetID(), rulesetName, owner, repo)
			id = ruleset.GetID()
		}
	}

	if id != 0 {
		ruleset, _, err := underlyingClient.Repositories.GetRuleset(ctx, owner, repo, id, true)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		return ruleset, nil
	}

	// Ruleset not found
	return nil, microerror.Maskf(rulesetNotFoundError, "ruleset %s not found in repository %s/%s", rulesetName, owner, repo)
}

// CreateRenovateRuleset creates a ruleset in the given repository that allows Renovate to merge approved PRs.
func (c *Client) CreateRenovateRuleset(ctx context.Context, owner, repo string) (*github.RepositoryRuleset, error) {
	c.logger.Infof("creating ruleset %s in repository %s/%s", rulesetName, owner, repo)

	underlyingClient := c.GetUnderlyingClient(ctx)

	ruleset, _, err := underlyingClient.Repositories.CreateRuleset(ctx, owner, repo, desiredRuleset)
	if err != nil {
		return nil, err
	}

	c.logger.Debugf("created ruleset %d in repository %s/%s", ruleset.GetID(), owner, repo)

	return ruleset, nil
}

func (c *Client) DeleteRuleset(ctx context.Context, owner, repo string, rulesetID int64) error {
	underlyingClient := c.GetUnderlyingClient(ctx)
	_, err := underlyingClient.Repositories.DeleteRuleset(ctx, owner, repo, rulesetID)
	if err != nil {
		return err
	}
	return nil
}

// Helper function to compare read and desired ruleset
func (c *Client) IsRulesetUpToDate(readRuleset github.RepositoryRuleset) bool {
	readRuleset.ID = nil
	readRuleset.CreatedAt = nil
	readRuleset.UpdatedAt = nil
	readRuleset.NodeID = nil
	readRuleset.Source = ""
	readRuleset.Links = nil
	if cmp.Equal(readRuleset, desiredRuleset) {
		return true
	} else {
		c.logger.Infof("Found these differences in ruleset:\n%s\n", cmp.Diff(desiredRuleset, readRuleset, nil))
		return false
	}
}
