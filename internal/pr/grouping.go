package pr

import (
	"regexp"
	"sort"
	"strings"
)

// PRGroup represents a group of related PRs based on dependency.
type PRGroup struct {
	DependencyName string
	PRs            []*PRInfo
	SearchQuery    string // Query to pass to existing search flow
}

// PRInfo contains essential information about a PR.
type PRInfo struct {
	Number int
	Owner  string
	Repo   string
	Title  string
	URL    string
}

// GroupRenovatePRs clusters PRs by dependency name.
// Returns groups sorted by PR count (descending).
// ALL groups are included, even those with only 1 PR.
func GroupRenovatePRs(prs []*PRInfo) []*PRGroup {
	// Map to hold PRs grouped by dependency name
	groups := make(map[string][]*PRInfo)

	// Group PRs by extracted dependency name
	for _, pr := range prs {
		depName := extractDependencyName(pr.Title)
		groups[depName] = append(groups[depName], pr)
	}

	// Convert map to slice of PRGroup
	var result []*PRGroup
	for depName, prList := range groups {
		// Use the first PR's title as a base for the search query
		// We'll extract a searchable string from the dependency name
		searchQuery := generateSearchQuery(depName, prList)

		result = append(result, &PRGroup{
			DependencyName: depName,
			PRs:            prList,
			SearchQuery:    searchQuery,
		})
	}

	// Sort by PR count (descending) - groups with most PRs first
	sort.Slice(result, func(i, j int) bool {
		return len(result[i].PRs) > len(result[j].PRs)
	})

	return result
}

// extractDependencyName applies clustering algorithms in sequence.
// Algorithm 1: Pattern-based extraction (primary)
// Algorithm 2: Version-stripped normalization (fallback)
// Algorithm 3: Exact title match (last resort)
func extractDependencyName(title string) string {
	// Algorithm 1: Pattern-Based Extraction
	patterns := []string{
		`[Uu]pdate dependency (@?[\w\-./]+(?:/[\w\-./]+)*) to`,
		`[Uu]pdate [Hh]elm [Rr]elease ([\w\-./]+) to`,
		`[Uu]pdate module ([\w\-./]+(?:/[\w\-./]+)*) to`,
		`[Uu]pdate ([\w\-./]+(?:/[\w\-./]+)*) digest to`,
		`chore\(deps\): update ([\w\-./]+(?:/[\w\-./]+)*) (?:docker tag|to)`,
		`[Uu]pdate ([\w\-./]+(?:/[\w\-./]+)*) action to`,
		// Generic patterns for other update formats
		`[Uu]pdate ([\w\-./]+(?:/[\w\-./]+)*) [Dd]ocker [Tt]ag to`,
		`[Uu]pdate ([\w\-./]+(?:/[\w\-./]+)*) to v?\d+`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(title)
		if len(matches) > 1 {
			return normalizeDepName(matches[1])
		}
	}

	// Algorithm 2: Version-Stripped Normalization
	normalized := normalizeWithVersionStrip(title)
	if normalized != "" {
		return normalized
	}

	// Algorithm 3: Exact Title Match (last resort)
	return title
}

// normalizeDepName normalizes the extracted dependency name.
func normalizeDepName(name string) string {
	// Remove version suffixes like /v80, /v2, etc. for better grouping
	re := regexp.MustCompile(`/v\d+$`)
	name = re.ReplaceAllString(name, "")

	return strings.TrimSpace(name)
}

// normalizeWithVersionStrip attempts to extract dependency name by stripping versions.
func normalizeWithVersionStrip(title string) string {
	// Remove common prefixes
	title = strings.TrimPrefix(title, "Update ")
	title = strings.TrimPrefix(title, "update ")
	title = strings.TrimPrefix(title, "chore(deps): ")
	title = strings.TrimPrefix(title, "chore(deps): update ")
	title = strings.TrimPrefix(title, "dependency ")
	title = strings.TrimPrefix(title, "module ")

	// Strip version patterns
	versionPatterns := []string{
		`v?\d+\.\d+\.\d+(?:-[\w.]+)?`, // Semantic versions
		`v\d+`,                         // Major versions only
		`\b[a-f0-9]{7,40}\b`,          // Git SHA/digest hashes
		`to v?\d+`,                     // "to v1.2.3"
		`\[SECURITY\]`,                 // Security tags
	}

	for _, pattern := range versionPatterns {
		re := regexp.MustCompile(pattern)
		title = re.ReplaceAllString(title, "")
	}

	// Clean up whitespace and take first meaningful tokens
	words := strings.Fields(title)
	if len(words) == 0 {
		return ""
	}

	// Take first 1-3 tokens that look like dependency names
	var tokens []string
	for _, word := range words {
		// Skip common words
		if isCommonWord(word) {
			continue
		}
		tokens = append(tokens, word)
		if len(tokens) >= 3 {
			break
		}
	}

	if len(tokens) == 0 {
		return ""
	}

	return strings.Join(tokens, " ")
}

// isCommonWord returns true if word is a common word to skip.
func isCommonWord(word string) bool {
	common := map[string]bool{
		"to":     true,
		"the":    true,
		"and":    true,
		"or":     true,
		"from":   true,
		"with":   true,
		"for":    true,
		"in":     true,
		"on":     true,
		"at":     true,
		"by":     true,
		"update": true,
		"docker": true,
		"tag":    true,
	}
	return common[strings.ToLower(word)]
}

// generateSearchQuery creates a search query string from the dependency name.
func generateSearchQuery(depName string, prs []*PRInfo) string {
	// Use the dependency name directly as the search query
	// The GitHub search will match this against PR titles
	return depName
}

