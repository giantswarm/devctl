package pr

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestExtractDependencyName(t *testing.T) {
	tests := []struct {
		name     string
		title    string
		expected string
	}{
		// Algorithm 1: Pattern-based extraction - Go modules
		{
			name:     "go module with version path",
			title:    "Update module github.com/google/go-github/v80 to v81",
			expected: "github.com/google/go-github",
		},
		{
			name:     "go module without version path",
			title:    "Update module github.com/prometheus/common to v0.67.5",
			expected: "github.com/prometheus/common",
		},
		// Algorithm 1: Pattern-based extraction - Docker images
		{
			name:     "docker image with digest",
			title:    "Update k8s.io/utils digest to 0fe9cd7",
			expected: "k8s.io/utils",
		},
		{
			name:     "docker tag with chore prefix",
			title:    "chore(deps): update ghcr.io/astral-sh/uv docker tag to v0.9.22",
			expected: "ghcr.io/astral-sh/uv",
		},
		// Algorithm 1: Pattern-based extraction - Helm releases
		{
			name:     "helm release",
			title:    "Update Helm release kube-prometheus-stack to v80.12.0",
			expected: "kube-prometheus-stack",
		},
		// Algorithm 1: Pattern-based extraction - Dependencies
		{
			name:     "npm dependency with scope",
			title:    "Update dependency @types/cors to v2.8.19",
			expected: "@types/cors",
		},
		{
			name:     "regular dependency",
			title:    "Update dependency storybook to v7.6.21 [SECURITY]",
			expected: "storybook",
		},
		// Algorithm 1: Pattern-based extraction - Actions
		{
			name:     "github action",
			title:    "Update giantswarm/install-binary-action action to v4",
			expected: "giantswarm/install-binary-action",
		},
		// Algorithm 1: Pattern-based extraction - Custom formats
		{
			name:     "custom update format with installation name",
			title:    "chore(deps): update grizzly to v33.1.1",
			expected: "grizzly",
		},
		{
			name:     "custom update format",
			title:    "Update gsoci.azurecr.io/giantswarm/kubectl-gs Docker tag to v4.9.1",
			expected: "gsoci.azurecr.io/giantswarm/kubectl-gs",
		},
		// Algorithm 2: Version-stripped normalization (fallback)
		{
			name:     "E2E tests dependencies (grouped)",
			title:    "Update E2E tests dependencies",
			expected: "E2E tests dependencies",
		},
		{
			name:     "monitoring stack (grouped)",
			title:    "chore(deps): update monitoring stack",
			expected: "monitoring stack",
		},
		{
			name:     "k8s modules (grouped)",
			title:    "Update k8s modules",
			expected: "k8s modules",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDependencyName(tt.title)
			if got != tt.expected {
				t.Errorf("extractDependencyName(%q) = %q, want %q", tt.title, got, tt.expected)
			}
		})
	}
}

func TestGroupRenovatePRs(t *testing.T) {
	prs := []*PRInfo{
		{Number: 1, Owner: "giantswarm", Repo: "repo1", Title: "Update module github.com/google/go-github/v80 to v81", URL: "https://github.com/giantswarm/repo1/pull/1"},
		{Number: 2, Owner: "giantswarm", Repo: "repo2", Title: "Update module github.com/google/go-github/v80 to v81", URL: "https://github.com/giantswarm/repo2/pull/2"},
		{Number: 3, Owner: "giantswarm", Repo: "repo3", Title: "Update module github.com/google/go-github/v80 to v81", URL: "https://github.com/giantswarm/repo3/pull/3"},
		{Number: 4, Owner: "giantswarm", Repo: "repo4", Title: "Update k8s.io/utils digest to 0fe9cd7", URL: "https://github.com/giantswarm/repo4/pull/4"},
		{Number: 5, Owner: "giantswarm", Repo: "repo5", Title: "Update k8s.io/utils digest to 0fe9cd7", URL: "https://github.com/giantswarm/repo5/pull/5"},
		{Number: 6, Owner: "giantswarm", Repo: "repo6", Title: "Update dependency storybook to v7.6.21 [SECURITY]", URL: "https://github.com/giantswarm/repo6/pull/6"},
	}

	groups := GroupRenovatePRs(prs)

	// Should have 3 groups
	if len(groups) != 3 {
		t.Fatalf("Expected 3 groups, got %d", len(groups))
	}

	// First group should have 3 PRs (go-github)
	if len(groups[0].PRs) != 3 {
		t.Errorf("First group should have 3 PRs, got %d", len(groups[0].PRs))
	}
	if groups[0].DependencyName != "github.com/google/go-github" {
		t.Errorf("First group should be github.com/google/go-github, got %s", groups[0].DependencyName)
	}

	// Second group should have 2 PRs (k8s.io/utils)
	if len(groups[1].PRs) != 2 {
		t.Errorf("Second group should have 2 PRs, got %d", len(groups[1].PRs))
	}
	if groups[1].DependencyName != "k8s.io/utils" {
		t.Errorf("Second group should be k8s.io/utils, got %s", groups[1].DependencyName)
	}

	// Third group should have 1 PR (storybook)
	if len(groups[2].PRs) != 1 {
		t.Errorf("Third group should have 1 PR, got %d", len(groups[2].PRs))
	}
	if groups[2].DependencyName != "storybook" {
		t.Errorf("Third group should be storybook, got %s", groups[2].DependencyName)
	}

	// Verify search queries are generated
	for i, group := range groups {
		if group.SearchQuery == "" {
			t.Errorf("Group %d has empty search query", i)
		}
	}
}

func TestGroupRenovatePRs_SinglePRGroups(t *testing.T) {
	// Test that single-PR groups are included
	prs := []*PRInfo{
		{Number: 1, Owner: "giantswarm", Repo: "repo1", Title: "Update dependency @types/cors to v2.8.19", URL: "https://github.com/giantswarm/repo1/pull/1"},
		{Number: 2, Owner: "giantswarm", Repo: "repo2", Title: "Update dependency storybook to v7.6.21", URL: "https://github.com/giantswarm/repo2/pull/2"},
	}

	groups := GroupRenovatePRs(prs)

	// Should have 2 groups, each with 1 PR
	if len(groups) != 2 {
		t.Fatalf("Expected 2 groups, got %d", len(groups))
	}

	for i, group := range groups {
		if len(group.PRs) != 1 {
			t.Errorf("Group %d should have 1 PR, got %d", i, len(group.PRs))
		}
	}
}

func TestGroupRenovatePRs_Sorting(t *testing.T) {
	// Test that groups are sorted by PR count (descending)
	prs := []*PRInfo{
		{Number: 1, Owner: "giantswarm", Repo: "repo1", Title: "Update dependency a to v1", URL: "url1"},
		{Number: 2, Owner: "giantswarm", Repo: "repo2", Title: "Update dependency b to v1", URL: "url2"},
		{Number: 3, Owner: "giantswarm", Repo: "repo3", Title: "Update dependency b to v1", URL: "url3"},
		{Number: 4, Owner: "giantswarm", Repo: "repo4", Title: "Update dependency b to v1", URL: "url4"},
		{Number: 5, Owner: "giantswarm", Repo: "repo5", Title: "Update dependency c to v1", URL: "url5"},
		{Number: 6, Owner: "giantswarm", Repo: "repo6", Title: "Update dependency c to v1", URL: "url6"},
	}

	groups := GroupRenovatePRs(prs)

	// Expected order: b (3 PRs), c (2 PRs), a (1 PR)
	expectedCounts := []int{3, 2, 1}
	for i, expected := range expectedCounts {
		if len(groups[i].PRs) != expected {
			t.Errorf("Group %d should have %d PRs, got %d", i, expected, len(groups[i].PRs))
		}
	}
}

func TestNormalizeDepName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "strips version path",
			input:    "github.com/google/go-github/v80",
			expected: "github.com/google/go-github",
		},
		{
			name:     "strips v2 version",
			input:    "github.com/giantswarm/clustertest/v2",
			expected: "github.com/giantswarm/clustertest",
		},
		{
			name:     "no version to strip",
			input:    "k8s.io/utils",
			expected: "k8s.io/utils",
		},
		{
			name:     "preserves other paths",
			input:    "github.com/aws/aws-sdk-go-v2/credentials",
			expected: "github.com/aws/aws-sdk-go-v2/credentials",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeDepName(tt.input)
			if got != tt.expected {
				t.Errorf("normalizeDepName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestGenerateSearchQuery(t *testing.T) {
	tests := []struct {
		name    string
		depName string
		prs     []*PRInfo
		want    string
	}{
		{
			name:    "simple dependency name",
			depName: "storybook",
			prs: []*PRInfo{
				{Title: "Update dependency storybook to v7.6.21 [SECURITY]"},
			},
			want: "storybook",
		},
		{
			name:    "scoped npm package",
			depName: "@types/cors",
			prs: []*PRInfo{
				{Title: "Update dependency @types/cors to v2.8.19"},
			},
			want: "@types/cors",
		},
		{
			name:    "go module path",
			depName: "github.com/google/go-github",
			prs: []*PRInfo{
				{Title: "Update module github.com/google/go-github/v80 to v81"},
			},
			want: "github.com/google/go-github",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateSearchQuery(tt.depName, tt.prs)
			if got != tt.want {
				t.Errorf("generateSearchQuery() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGroupRenovatePRs_Integration(t *testing.T) {
	// Test with real-world PR titles from the search results
	prs := []*PRInfo{
		{Number: 4686, Repo: "happa", Title: "Update dependency @types/cors to v2.8.19"},
		{Number: 4687, Repo: "happa", Title: "Update dependency storybook to v7.6.21 [SECURITY]"},
		{Number: 1072, Repo: "capi-image-builder", Title: "Update module github.com/google/go-github/v80 to v81"},
		{Number: 224, Repo: "pr-comment-filter", Title: "Update module github.com/google/go-github/v80 to v81"},
		{Number: 231, Repo: "pr-gatekeeper", Title: "Update module github.com/google/go-github/v80 to v81"},
		{Number: 772, Repo: "operatorkit", Title: "Update k8s.io/utils digest to 0fe9cd7"},
		{Number: 896, Repo: "cluster-test-suites", Title: "Update k8s.io/utils digest to 0fe9cd7"},
		{Number: 451, Repo: "app-build-suite", Title: "chore(deps): update ghcr.io/astral-sh/uv docker tag to v0.9.22"},
		{Number: 503, Repo: "kube-prometheus-stack-app", Title: "Update Helm release kube-prometheus-stack to v80.12.0"},
		{Number: 156, Repo: "tekton-dashboard-loki-proxy", Title: "Update module github.com/prometheus/common to v0.67.5"},
	}

	groups := GroupRenovatePRs(prs)

	// We expect these groups:
	// - github.com/google/go-github: 3 PRs
	// - k8s.io/utils: 2 PRs
	// - @types/cors: 1 PR
	// - storybook: 1 PR
	// - ghcr.io/astral-sh/uv: 1 PR
	// - kube-prometheus-stack: 1 PR
	// - github.com/prometheus/common: 1 PR
	expectedGroupCount := 7
	if len(groups) != expectedGroupCount {
		t.Errorf("Expected %d groups, got %d", expectedGroupCount, len(groups))
		for i, g := range groups {
			t.Logf("Group %d: %s (%d PRs)", i, g.DependencyName, len(g.PRs))
		}
	}

	// First group should have the most PRs
	if len(groups) > 0 {
		firstGroupPRCount := len(groups[0].PRs)
		for i := 1; i < len(groups); i++ {
			if len(groups[i].PRs) > firstGroupPRCount {
				t.Errorf("Groups not sorted correctly: group 0 has %d PRs, but group %d has %d PRs",
					firstGroupPRCount, i, len(groups[i].PRs))
			}
		}
	}

	// Verify go-github group has 3 PRs
	var goGithubGroup *PRGroup
	for _, g := range groups {
		if g.DependencyName == "github.com/google/go-github" {
			goGithubGroup = g
			break
		}
	}
	if goGithubGroup == nil {
		t.Fatal("Expected to find github.com/google/go-github group")
	}
	if len(goGithubGroup.PRs) != 3 {
		t.Errorf("Expected go-github group to have 3 PRs, got %d", len(goGithubGroup.PRs))
	}

	// Verify single-PR groups are included
	var singlePRGroups []*PRGroup
	for _, g := range groups {
		if len(g.PRs) == 1 {
			singlePRGroups = append(singlePRGroups, g)
		}
	}
	if len(singlePRGroups) != 5 {
		t.Errorf("Expected 5 single-PR groups, got %d", len(singlePRGroups))
	}
}

func TestGroupRenovatePRs_EmptyInput(t *testing.T) {
	groups := GroupRenovatePRs([]*PRInfo{})
	if len(groups) != 0 {
		t.Errorf("Expected 0 groups for empty input, got %d", len(groups))
	}
}

func TestNormalizeWithVersionStrip(t *testing.T) {
	tests := []struct {
		name     string
		title    string
		expected string
	}{
		{
			name:     "k8s modules",
			title:    "Update k8s modules",
			expected: "k8s modules",
		},
		{
			name:     "monitoring stack",
			title:    "chore(deps): update monitoring stack",
			expected: "monitoring stack",
		},
		{
			name:     "E2E tests dependencies",
			title:    "Update E2E tests dependencies",
			expected: "E2E tests dependencies",
		},
		{
			name:     "strips semantic version",
			title:    "Update something to v1.2.3",
			expected: "something",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeWithVersionStrip(tt.title)
			if got != tt.expected {
				t.Errorf("normalizeWithVersionStrip(%q) = %q, want %q", tt.title, got, tt.expected)
			}
		})
	}
}

func TestIsCommonWord(t *testing.T) {
	common := []string{"to", "the", "and", "update", "docker", "tag"}
	for _, word := range common {
		if !isCommonWord(word) {
			t.Errorf("Expected %q to be a common word", word)
		}
	}

	notCommon := []string{"storybook", "k8s", "prometheus", "github"}
	for _, word := range notCommon {
		if isCommonWord(word) {
			t.Errorf("Expected %q to NOT be a common word", word)
		}
	}
}

// TestGroupRenovatePRs_Stability verifies that grouping is deterministic
func TestGroupRenovatePRs_Stability(t *testing.T) {
	prs := []*PRInfo{
		{Number: 1, Title: "Update dependency a to v1"},
		{Number: 2, Title: "Update dependency b to v1"},
		{Number: 3, Title: "Update dependency a to v1"},
	}

	// Run grouping multiple times
	results := make([][]*PRGroup, 10)
	for i := 0; i < 10; i++ {
		results[i] = GroupRenovatePRs(prs)
	}

	// All results should be identical
	for i := 1; i < len(results); i++ {
		if diff := cmp.Diff(results[0], results[i]); diff != "" {
			t.Errorf("Grouping is not deterministic, iteration %d differs (-want +got):\n%s", i, diff)
		}
	}
}

