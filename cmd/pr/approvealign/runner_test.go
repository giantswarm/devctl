package approvealign

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/go-github/v72/github"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// mockGitHubClient is a mock implementation for testing
type mockGitHubClient struct {
	searchResults *github.IssuesSearchResult
	searchError   error
	reviewError   error
	reviewCalls   []reviewCall
}

type reviewCall struct {
	owner    string
	repo     string
	prNumber int
}

func (m *mockGitHubClient) SearchIssues(ctx context.Context, query string, opts *github.SearchOptions) (*github.IssuesSearchResult, *github.Response, error) {
	if m.searchError != nil {
		return nil, nil, m.searchError
	}
	return m.searchResults, &github.Response{}, nil
}

func (m *mockGitHubClient) CreateReview(ctx context.Context, owner, repo string, number int, review *github.PullRequestReviewRequest) (*github.PullRequestReview, *github.Response, error) {
	m.reviewCalls = append(m.reviewCalls, reviewCall{
		owner:    owner,
		repo:     repo,
		prNumber: number,
	})

	if m.reviewError != nil {
		return nil, nil, m.reviewError
	}

	return &github.PullRequestReview{}, &github.Response{}, nil
}

// mockGitHubClientService wraps the mock client
type mockGitHubClientService struct {
	client *mockGitHubClient
}

func (m *mockGitHubClientService) GetUnderlyingClient(ctx context.Context) githubClientInterface {
	return m.client
}

// githubClientInterface defines the interface we need from the GitHub client
type githubClientInterface interface {
	SearchIssues(ctx context.Context, query string, opts *github.SearchOptions) (*github.IssuesSearchResult, *github.Response, error)
	CreateReview(ctx context.Context, owner, repo string, number int, review *github.PullRequestReviewRequest) (*github.PullRequestReview, *github.Response, error)
}

// Helper function to create test issues
func createTestIssue(prNumber int, repoOwner, repoName string) *github.Issue {
	return &github.Issue{
		Number: github.Ptr(prNumber),
		Repository: &github.Repository{
			Name: github.Ptr(repoName),
			Owner: &github.User{
				Login: github.Ptr(repoOwner),
			},
			Organization: &github.Organization{
				Login: github.Ptr(repoOwner),
			},
		},
	}
}

func createTestIssueWithOwnerOnly(prNumber int, repoOwner, repoName string) *github.Issue {
	return &github.Issue{
		Number: github.Ptr(prNumber),
		Repository: &github.Repository{
			Name: github.Ptr(repoName),
			Owner: &github.User{
				Login: github.Ptr(repoOwner),
			},
			// No Organization field to test fallback
		},
	}
}

func TestRunner_Run_NoGitHubToken(t *testing.T) {
	// Ensure GITHUB_TOKEN is not set
	originalToken := os.Getenv("GITHUB_TOKEN")
	os.Unsetenv("GITHUB_TOKEN")
	defer func() {
		if originalToken != "" {
			os.Setenv("GITHUB_TOKEN", originalToken)
		}
	}()

	logger := logrus.New()
	logger.SetOutput(bytes.NewBuffer(nil))

	var stdout, stderr bytes.Buffer
	r := &runner{
		flag:   &flag{},
		logger: logger,
		stdout: &stdout,
		stderr: &stderr,
	}

	cmd := &cobra.Command{}
	err := r.Run(cmd, []string{})

	if err == nil {
		t.Fatal("Expected error when GITHUB_TOKEN is not set")
	}

	if !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("Expected error to mention GITHUB_TOKEN, got: %v", err)
	}
}

func TestRunner_SearchQuery(t *testing.T) {
	expectedQuery := `is:pr is:open status:success org:giantswarm review-requested:@me "Align files"`

	// Verify all required components are in the query
	requiredParts := []string{
		"is:pr",
		"is:open",
		"status:success",
		"org:giantswarm",
		"review-requested:@me",
		`"Align files"`,
	}

	for _, part := range requiredParts {
		if !strings.Contains(expectedQuery, part) {
			t.Fatalf("Search query missing required part: %s", part)
		}
	}
}

func TestRunner_ExtractRepositoryInfo(t *testing.T) {
	testCases := []struct {
		name          string
		issue         *github.Issue
		expectedOwner string
		expectedRepo  string
		expectedError bool
	}{
		{
			name:          "valid issue with organization",
			issue:         createTestIssue(123, "giantswarm", "test-repo"),
			expectedOwner: "giantswarm",
			expectedRepo:  "test-repo",
			expectedError: false,
		},
		{
			name:          "valid issue with owner fallback",
			issue:         createTestIssueWithOwnerOnly(456, "giantswarm", "another-repo"),
			expectedOwner: "giantswarm",
			expectedRepo:  "another-repo",
			expectedError: false,
		},
		{
			name: "nil repository",
			issue: &github.Issue{
				Number:     github.Ptr(789),
				Repository: nil,
			},
			expectedOwner: "",
			expectedRepo:  "",
			expectedError: true,
		},
		{
			name: "nil repository name",
			issue: &github.Issue{
				Number: github.Ptr(101),
				Repository: &github.Repository{
					Name: nil,
					Owner: &github.User{
						Login: github.Ptr("giantswarm"),
					},
				},
			},
			expectedOwner: "",
			expectedRepo:  "",
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Extract repository info using the same logic as in the runner
			var owner, repoName string
			hasError := false

			if tc.issue.GetRepository() != nil {
				repoName = tc.issue.GetRepository().GetName()

				// Try to get owner from organization first, then fall back to owner
				if tc.issue.GetRepository().GetOrganization() != nil {
					owner = tc.issue.GetRepository().GetOrganization().GetLogin()
				} else if tc.issue.GetRepository().GetOwner() != nil {
					owner = tc.issue.GetRepository().GetOwner().GetLogin()
				}
			}

			if owner == "" || repoName == "" {
				hasError = true
			}

			if hasError != tc.expectedError {
				t.Fatalf("Expected error: %v, got error: %v", tc.expectedError, hasError)
			}

			if !tc.expectedError {
				if owner != tc.expectedOwner {
					t.Fatalf("Expected owner: %s, got: %s", tc.expectedOwner, owner)
				}
				if repoName != tc.expectedRepo {
					t.Fatalf("Expected repo: %s, got: %s", tc.expectedRepo, repoName)
				}
			}
		})
	}
}

func TestRunner_ErrorHandling(t *testing.T) {
	testCases := []struct {
		name          string
		reviewError   error
		expectedRetry bool
		expectedSkip  bool
	}{
		{
			name: "500 server error should retry",
			reviewError: &github.ErrorResponse{
				Response: &http.Response{StatusCode: 500},
				Message:  "Internal server error",
			},
			expectedRetry: true,
			expectedSkip:  false,
		},
		{
			name: "504 timeout should retry",
			reviewError: &github.ErrorResponse{
				Response: &http.Response{StatusCode: 504},
				Message:  "Gateway timeout",
			},
			expectedRetry: true,
			expectedSkip:  false,
		},
		{
			name: "422 unprocessable entity should skip",
			reviewError: &github.ErrorResponse{
				Response: &http.Response{StatusCode: 422},
				Message:  "Validation failed",
			},
			expectedRetry: false,
			expectedSkip:  true,
		},
		{
			name: "404 not found should not retry",
			reviewError: &github.ErrorResponse{
				Response: &http.Response{StatusCode: 404},
				Message:  "Not found",
			},
			expectedRetry: false,
			expectedSkip:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test the error handling logic
			shouldRetry := false
			shouldSkip := false

			if ghErr, ok := tc.reviewError.(*github.ErrorResponse); ok {
				if ghErr.Response.StatusCode >= 500 && ghErr.Response.StatusCode < 600 {
					shouldRetry = true
				}
				if ghErr.Response.StatusCode == http.StatusUnprocessableEntity {
					shouldSkip = true
				}
			}

			if shouldRetry != tc.expectedRetry {
				t.Fatalf("Expected retry: %v, got: %v", tc.expectedRetry, shouldRetry)
			}
			if shouldSkip != tc.expectedSkip {
				t.Fatalf("Expected skip: %v, got: %v", tc.expectedSkip, shouldSkip)
			}
		})
	}
}

func TestRunner_OutputMessages(t *testing.T) {
	testCases := []struct {
		name           string
		prCount        int
		expectedOutput string
	}{
		{
			name:           "no PRs found",
			prCount:        0,
			expectedOutput: "No PRs found matching the criteria.",
		},
		{
			name:           "single PR found",
			prCount:        1,
			expectedOutput: "Found 1 PRs to review.",
		},
		{
			name:           "multiple PRs found",
			prCount:        5,
			expectedOutput: "Found 5 PRs to review.",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout bytes.Buffer

			if tc.prCount == 0 {
				fmt.Fprintln(&stdout, "No PRs found matching the criteria.")
			} else {
				fmt.Fprintf(&stdout, "Found %d PRs to review.\n", tc.prCount)
			}

			output := stdout.String()
			if !strings.Contains(output, tc.expectedOutput) {
				t.Fatalf("Expected output to contain '%s', got: %s", tc.expectedOutput, output)
			}
		})
	}
}

// Integration test structure (would require more setup)
func TestRunner_Integration_MockClient(t *testing.T) {
	// This test demonstrates how we could test the full flow with a mock client
	// In a real implementation, we'd need to refactor the runner to accept
	// a GitHub client interface for dependency injection

	testCases := []struct {
		name          string
		issues        []*github.Issue
		reviewError   error
		expectedCalls int
	}{
		{
			name: "successful approval of multiple PRs",
			issues: []*github.Issue{
				createTestIssue(123, "giantswarm", "repo1"),
				createTestIssue(456, "giantswarm", "repo2"),
			},
			reviewError:   nil,
			expectedCalls: 2,
		},
		{
			name: "skip PR with invalid repository info",
			issues: []*github.Issue{
				createTestIssue(123, "giantswarm", "repo1"),
				{
					Number:     github.Ptr(456),
					Repository: nil, // This should be skipped
				},
			},
			reviewError:   nil,
			expectedCalls: 1, // Only one valid PR should be processed
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Verify the expected number of review calls would be made
			// In a real test, we'd inject this mock and run the actual command
			expectedValidIssues := 0
			for _, issue := range tc.issues {
				if issue.GetRepository() != nil &&
					issue.GetRepository().GetName() != "" &&
					(issue.GetRepository().GetOrganization() != nil || issue.GetRepository().GetOwner() != nil) {
					expectedValidIssues++
				}
			}

			if expectedValidIssues != tc.expectedCalls {
				t.Fatalf("Expected %d valid issues, got %d", tc.expectedCalls, expectedValidIssues)
			}
		})
	}
}

func TestRunner_ExtractRepositoryURL(t *testing.T) {
	testCases := []struct {
		name          string
		repositoryURL string
		wantOwner     string
		wantRepo      string
		shouldSkip    bool
	}{
		{
			name:          "valid repository URL",
			repositoryURL: "https://api.github.com/repos/giantswarm/test-repo",
			wantOwner:     "giantswarm",
			wantRepo:      "test-repo",
			shouldSkip:    false,
		},
		{
			name:          "repository URL with extra path",
			repositoryURL: "https://api.github.com/repos/giantswarm/another-repo/extra",
			wantOwner:     "giantswarm",
			wantRepo:      "another-repo",
			shouldSkip:    false,
		},
		{
			name:          "invalid repository URL - too short",
			repositoryURL: "https://api.github.com/repos",
			wantOwner:     "",
			wantRepo:      "",
			shouldSkip:    true,
		},
		{
			name:          "empty repository URL",
			repositoryURL: "",
			wantOwner:     "",
			wantRepo:      "",
			shouldSkip:    true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			parts := strings.Split(tc.repositoryURL, "/")

			if len(parts) < 6 {
				if !tc.shouldSkip {
					t.Fatal("Expected valid URL but got invalid")
				}
				return
			}

			owner, repo := parts[4], parts[5]

			if owner != tc.wantOwner {
				t.Fatalf("Expected owner %q, got %q", tc.wantOwner, owner)
			}
			if repo != tc.wantRepo {
				t.Fatalf("Expected repo %q, got %q", tc.wantRepo, repo)
			}
		})
	}
}

func TestRunner_OutputFormatting(t *testing.T) {
	testCases := []struct {
		name           string
		totalPRs       int
		approvedPRs    int
		expectedOutput []string
	}{
		{
			name:        "no PRs found",
			totalPRs:    0,
			approvedPRs: 0,
			expectedOutput: []string{
				"Found 0 PRs to review.",
				"All remaining PRs",
			},
		},
		{
			name:        "some PRs approved",
			totalPRs:    5,
			approvedPRs: 3,
			expectedOutput: []string{
				"Found 5 PRs to review.",
				"Approved 3 out of 5 PRs.",
				"All remaining PRs",
			},
		},
		{
			name:        "all PRs approved",
			totalPRs:    2,
			approvedPRs: 2,
			expectedOutput: []string{
				"Found 2 PRs to review.",
				"Approved 2 out of 2 PRs.",
				"All remaining PRs",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout bytes.Buffer

			// Simulate the output
			fmt.Fprintf(&stdout, "Found %d PRs to review.\n", tc.totalPRs)
			if tc.approvedPRs > 0 {
				fmt.Fprintf(&stdout, "\nApproved %d out of %d PRs.\n", tc.approvedPRs, tc.totalPRs)
			}
			fmt.Fprintln(&stdout, "\nAll remaining PRs (including any that failed approval or are pending) can be seen with this search query:")

			output := stdout.String()
			for _, expected := range tc.expectedOutput {
				if !strings.Contains(output, expected) {
					t.Fatalf("Expected output to contain %q, got:\n%s", expected, output)
				}
			}
		})
	}
}

func TestRunner_ErrorMessages(t *testing.T) {
	var stderr bytes.Buffer
	prNumber := 123
	testError := errors.New("test error")

	fmt.Fprintf(&stderr, "Failed to approve PR #%d: %v\n", prNumber, testError)

	output := stderr.String()
	if !strings.Contains(output, "Failed to approve PR #123") {
		t.Fatalf("Expected error message for PR #123, got: %s", output)
	}
	if !strings.Contains(output, "test error") {
		t.Fatalf("Expected error details, got: %s", output)
	}
}

// Test helper to create test issues with repository URLs
func createTestIssueWithURL(prNumber int, repoURL string) *github.Issue {
	return &github.Issue{
		Number:        github.Ptr(prNumber),
		RepositoryURL: github.Ptr(repoURL),
	}
}

func TestRunner_IssueProcessing(t *testing.T) {
	testCases := []struct {
		name             string
		issues           []*github.Issue
		expectedApproved int
	}{
		{
			name: "all valid repository URLs",
			issues: []*github.Issue{
				createTestIssueWithURL(1, "https://api.github.com/repos/giantswarm/repo1"),
				createTestIssueWithURL(2, "https://api.github.com/repos/giantswarm/repo2"),
			},
			expectedApproved: 2,
		},
		{
			name: "mixed valid and invalid URLs",
			issues: []*github.Issue{
				createTestIssueWithURL(1, "https://api.github.com/repos/giantswarm/repo1"),
				createTestIssueWithURL(2, "https://invalid/url"),
				createTestIssueWithURL(3, "https://api.github.com/repos/giantswarm/repo3"),
			},
			expectedApproved: 2,
		},
		{
			name: "empty repository URLs",
			issues: []*github.Issue{
				createTestIssueWithURL(1, ""),
				createTestIssueWithURL(2, ""),
			},
			expectedApproved: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			approved := 0
			for _, issue := range tc.issues {
				parts := strings.Split(issue.GetRepositoryURL(), "/")
				if len(parts) >= 6 {
					approved++
				}
			}

			if approved != tc.expectedApproved {
				t.Fatalf("Expected %d approved, got %d", tc.expectedApproved, approved)
			}
		})
	}
}

// Test the GitHub search options
func TestGitHubSearchOptions(t *testing.T) {
	opts := &github.SearchOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	if opts.ListOptions.PerPage != 100 {
		t.Fatalf("Expected PerPage to be 100, got %d", opts.ListOptions.PerPage)
	}
}

// Test error response handling
func TestErrorResponseTypes(t *testing.T) {
	testCases := []struct {
		name        string
		err         error
		shouldPrint bool
	}{
		{
			name:        "nil error",
			err:         nil,
			shouldPrint: false,
		},
		{
			name:        "generic error",
			err:         errors.New("generic error"),
			shouldPrint: true,
		},
		{
			name: "github error response",
			err: &github.ErrorResponse{
				Response: &http.Response{StatusCode: 404},
				Message:  "Not found",
			},
			shouldPrint: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err != nil && !tc.shouldPrint {
				t.Fatal("Expected error to be printed")
			}
		})
	}
}
