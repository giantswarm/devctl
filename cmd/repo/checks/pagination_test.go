package checks

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/go-github/v93/github"
	"github.com/stretchr/testify/require"
)

// rulesetPages serves a legal 31-ruleset repository, with the engine's ruleset
// on the second page. Both the command's preflight and the engine must find it.
type rulesetPages struct {
	fixture *checksFixture
	passes  int
	pages   []int
	failOn  int
}

func (p *rulesetPages) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet || req.URL.Path != fixtureRepoPath+"/rulesets" {
		return p.fixture.RoundTrip(req)
	}
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	if req.URL.Host != "api.github.com" || req.URL.Query().Get("includes_parents") != "false" {
		return nil, fmt.Errorf("unexpected ruleset listing destination or parent scope")
	}
	page := 1
	if req.URL.Query().Get("page") == "2" {
		page = 2
	}
	p.pages = append(p.pages, page)
	if page == 2 {
		if p.passes == p.failOn {
			return p.fixture.response(req, http.StatusServiceUnavailable, map[string]string{"message": "fixture list unavailable"})
		}
		return p.fixture.response(req, http.StatusOK, []*github.RepositoryRuleset{p.fixture.ruleset})
	}
	p.passes++
	var summaries []*github.RepositoryRuleset
	for i := range 30 {
		summaries = append(summaries, &github.RepositoryRuleset{ID: new(int64(100 + i)), Name: fmt.Sprintf("fixture rule %d", i), Enforcement: github.RulesetEnforcementDisabled})
	}
	resp, err := p.fixture.response(req, http.StatusOK, summaries)
	if err != nil {
		return nil, err
	}
	resp.Header.Set("Link", `<https://api.github.com/repos/fixture/repository/rulesets?page=2&includes_parents=false>; rel="next"`)
	return resp, nil
}

func TestCommandRulesetPagination(t *testing.T) {
	for _, tc := range []struct {
		name   string
		update bool
		failOn int
	}{
		{name: "case 0: check a ruleset on page two"},
		{name: "case 1: update a ruleset on page two", update: true},
		{name: "case 2: preflight page two fails", update: true, failOn: 1},
		{name: "case 3: engine page two fails", update: true, failOn: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newChecksFixture(t)
			f.protection = nil
			f.ruleset = fixtureRuleset(&github.RuleStatusCheck{Context: "existing gate", IntegrationID: new(int64(4321))})
			f.reported = []string{"ci/circleci: build"}
			paged := &rulesetPages{fixture: f, failOn: tc.failOn}
			http.DefaultTransport = paged
			args := []string{"--output=json", "fixture/repository"}
			if tc.update {
				args = append([]string{"--update"}, args...)
			}
			output, err := f.execute(t.Context(), args...)
			if tc.failOn != 0 {
				require.Error(t, err)
				require.Contains(t, err.Error()+output, "fixture list unavailable")
				require.Empty(t, f.writes)
				if tc.failOn == 1 {
					require.Equal(t, []int{1, 2}, paged.pages)
				} else {
					require.Equal(t, []int{1, 2, 1, 2}, paged.pages)
					require.Contains(t, output, `"verdict": "failed"`)
				}
				return
			}
			require.NoError(t, err)
			require.Equal(t, []int{1, 2, 1, 2}, paged.pages, "both lookups must consume the complete listing")
			if !tc.update {
				require.Contains(t, output, `"verdict": "drift"`)
				require.Empty(t, f.writes)
				return
			}
			require.Len(t, f.writes, 1)
			require.Equal(t, "/rulesets/7", f.writes[0].path)
			require.Equal(t, http.MethodPut, f.writes[0].method)
			require.Equal(t, []*github.RuleStatusCheck{{Context: "existing gate", IntegrationID: new(int64(4321))}, {Context: "ci/circleci: build"}}, f.ruleset.Rules.RequiredStatusChecks.RequiredStatusChecks)
		})
	}
}
