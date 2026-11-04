package approvealign

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/internal/pr"
)

const (
	failedPRRead     = "Failed to get PR"
	failedReviewRead = "Failed to get reviews"
)

// readErrorStatus keeps GitHub's status and message without the request URL.
// Transport and decode errors still carry their original explanation.
func readErrorStatus(operation string, resp *github.Response, err error) string {
	var message string
	var apiError *github.ErrorResponse
	var rateError *github.RateLimitError
	var abuseError *github.AbuseRateLimitError
	switch {
	case errors.As(err, &apiError):
		message = apiError.Message
	case errors.As(err, &rateError):
		message = rateError.Message
	case errors.As(err, &abuseError):
		message = abuseError.Message
	default:
		message = err.Error()
	}
	// Keep each diagnostic on one terminal row, including multiline API messages.
	message = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, message)
	message = strings.Join(strings.Fields(message), " ")
	if resp != nil && resp.Response != nil {
		if message == "" {
			message = http.StatusText(resp.StatusCode)
		}
		return fmt.Sprintf("%s: HTTP %d %s", operation, resp.StatusCode, message)
	}
	return operation + ": " + message
}

type readErrorGroup struct {
	status string
	count  int
}

// groupReadErrors preserves search order and groups identical diagnostics.
// commonCode is nonzero only when every PR failed a read with that HTTP status.
func groupReadErrors(statuses []*pr.PRStatus) (groups []readErrorGroup, commonCode int) {
	indices := map[string]int{}
	readFailures := 0
	for _, ps := range statuses {
		status := ps.GetStatus()
		var detail string
		if strings.HasPrefix(status, failedPRRead+": ") {
			detail = strings.TrimPrefix(status, failedPRRead+": ")
		} else if strings.HasPrefix(status, failedReviewRead+": ") {
			detail = strings.TrimPrefix(status, failedReviewRead+": ")
		} else {
			continue
		}
		readFailures++
		if i, ok := indices[status]; ok {
			groups[i].count++
		} else {
			indices[status] = len(groups)
			groups = append(groups, readErrorGroup{status: status, count: 1})
		}
		code := 0
		if strings.HasPrefix(detail, "HTTP ") {
			codeText, _, _ := strings.Cut(strings.TrimPrefix(detail, "HTTP "), " ")
			code, _ = strconv.Atoi(codeText)
		}
		if readFailures == 1 {
			commonCode = code
		} else if code != commonCode {
			commonCode = 0
		}
	}
	if readFailures != len(statuses) {
		commonCode = 0
	}
	return groups, commonCode
}

// updateStatusTable replaces the previous display. A shared HTTP failure gets
// one batch diagnostic rather than a table repeating the same error per PR.
func (r *runner) updateStatusTable(statuses []*pr.PRStatus, previousRows int, tokenSource string) int {
	if previousRows > 0 {
		fmt.Fprintf(r.stdout, "\033[%dA\033[J", previousRows)
	}
	groups, commonCode := groupReadErrors(statuses)
	if commonCode != 0 {
		fmt.Fprintf(r.stdout, "All %d PRs failed GitHub reads with HTTP %d.\n", len(statuses), commonCode)
		rows := 1
		if tokenSource != "" {
			fmt.Fprintf(r.stdout, "Token source: %s\n", tokenSource)
			rows++
		}
		if commonCode == http.StatusUnauthorized || commonCode == http.StatusForbidden || commonCode == http.StatusNotFound {
			fmt.Fprintln(r.stdout, "Check token validity, repository permissions, App installation access and GitHub rate limits.")
			rows++
		}
		for _, group := range groups {
			fmt.Fprintf(r.stdout, "  %d PR(s): %s\n", group.count, group.status)
			rows++
		}
		return rows
	}

	pr.PrintTableHeader(r.stdout, "Repository")
	for range statuses {
		fmt.Fprintln(r.stdout, "")
	}
	pr.UpdateTable(r.stdout, statuses)
	return len(statuses) + 2
}
