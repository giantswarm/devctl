package client

import (
	"fmt"
	"io"
	"strings"

	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

const timeFormat = "2006-01-02T15:04:05Z"

// PrintRecord writes an inventory record: the declaration, the reality on
// GitHub, CircleCI, the CI configuration, Renovate, the catalog and the
// mapping, the set-up state with its steps and runs, and the findings.
func PrintRecord(w io.Writer, r *manager.Record) {
	head := r.Repository
	if r.Source != "" || r.Age != "" {
		head += fmt.Sprintf(" (record from %s, %s old)", r.Source, r.Age)
	}
	fmt.Fprintln(w, head)

	if d := r.Declaration; d == nil {
		fmt.Fprintln(w, "declaration: none -- no team file declares this repository (the Unassigned scope); `devctl repo adopt` declares it")
	} else {
		verdict := "accepted"
		if !d.Accepted {
			verdict = "refused"
		}
		parts := []string{}
		if d.ComponentType != "" {
			parts = append(parts, d.ComponentType)
		}
		if d.Language != "" {
			parts = append(parts, d.Language)
		}
		if len(d.Flavours) > 0 {
			parts = append(parts, "flavours "+strings.Join(d.Flavours, ", "))
		}
		lifecycle := d.Lifecycle
		if lifecycle == "" {
			lifecycle = "production"
		}
		parts = append(parts, "lifecycle "+lifecycle, verdict)
		fmt.Fprintf(w, "declaration: %s (%s): %s\n", d.Team, d.File, strings.Join(parts, ", "))
		for _, p := range d.Problems {
			fmt.Fprintf(w, "  %s\n", p)
		}
		if d.Entry != "" {
			fmt.Fprintln(w, Indent(d.Entry, "  "))
		}
	}

	if re := r.Reality; re == nil {
		fmt.Fprintln(w, "github: gone -- the repository does not exist on GitHub")
	} else {
		parts := []string{re.URL}
		if re.Visibility != "" {
			parts = append(parts, re.Visibility)
		}
		if re.IsArchived {
			parts = append(parts, "archived")
		}
		if re.IsFork {
			parts = append(parts, "fork")
		}
		if re.IsEmpty {
			parts = append(parts, "empty")
		}
		if re.DefaultBranch != "" {
			parts = append(parts, "default branch "+re.DefaultBranch)
		}
		if c := re.LastPersonCommit; c != nil {
			parts = append(parts, fmt.Sprintf("last person commit %s by %s", c.Date.UTC().Format("2006-01-02"), c.Author))
		} else if re.LastCommit != nil {
			parts = append(parts, "no person commit in the sampled history")
		}
		if rel := re.LatestRelease; rel != nil {
			release := "latest release " + rel.Tag
			if rel.Build != nil {
				release += " (build " + rel.Build.State + ")"
			}
			parts = append(parts, release)
		}
		fmt.Fprintf(w, "github: %s\n", strings.Join(parts, " · "))
	}

	if c := r.CircleCI; c != nil {
		parts := []string{}
		parts = append(parts, "followed "+yesNoUnknown(c.Followed), "setup workflows "+yesNoUnknown(c.SetupWorkflows))
		if c.Webhook != nil {
			parts = append(parts, "webhook "+presence(*c.Webhook))
		}
		if c.Head != nil {
			parts = append(parts, "head "+c.Head.State)
		}
		if c.Source != "" {
			parts = append(parts, "from "+c.Source)
		}
		if c.Error != "" {
			parts = append(parts, "error: "+c.Error)
		}
		fmt.Fprintf(w, "circleci: %s\n", strings.Join(parts, ", "))
	}
	if ci := r.CI; ci != nil {
		parts := []string{}
		if ci.Generated {
			parts = append(parts, "generated")
		} else {
			parts = append(parts, "hand-written")
		}
		if ci.Orb != "" {
			parts = append(parts, "orb "+ci.Orb)
		}
		parts = append(parts, "arm64 "+yesNoUnknown(ci.ARM64))
		if ci.ChinaPush != "" {
			parts = append(parts, "china push "+ci.ChinaPush)
		}
		if ci.Signing != "" {
			signing := "signing " + ci.Signing
			if ci.SigningReason != "" {
				signing += " (" + ci.SigningReason + ")"
			}
			parts = append(parts, signing)
		}
		if ci.Error != "" {
			parts = append(parts, "error: "+ci.Error)
		}
		fmt.Fprintf(w, "ci: %s\n", strings.Join(parts, ", "))
	}
	if rn := r.Renovate; rn != nil {
		parts := []string{}
		switch {
		case !rn.Configured:
			parts = append(parts, "not configured")
		case !rn.Enabled:
			parts = append(parts, "configured ("+rn.Path+") but disabled")
		default:
			parts = append(parts, "configured ("+rn.Path+")")
		}
		if rn.DashboardIssue != nil {
			parts = append(parts, fmt.Sprintf("dashboard #%d", rn.DashboardIssue.Number))
		}
		if pr := rn.LastPullRequest; pr != nil {
			parts = append(parts, fmt.Sprintf("last pull request #%d (%s)", pr.Number, pr.CreatedAt.UTC().Format("2006-01-02")))
		}
		if rn.LastCommit != nil {
			parts = append(parts, "last commit "+rn.LastCommit.UTC().Format("2006-01-02"))
		}
		fmt.Fprintf(w, "renovate: %s\n", strings.Join(parts, ", "))
	}
	if r.Catalog != nil || r.Mapping != nil {
		parts := []string{}
		if r.Catalog != nil {
			parts = append(parts, "catalog "+presence(r.Catalog.Present))
		}
		if r.Mapping != nil {
			m := "mapping " + presence(r.Mapping.Present)
			if r.Mapping.Team != "" {
				m += " (team " + r.Mapping.Team + ")"
			}
			parts = append(parts, m)
		}
		fmt.Fprintln(w, strings.Join(parts, " · "))
	}

	s := r.Setup
	switch {
	case s.Checks != nil:
		checked := ""
		if !s.CheckedAt.IsZero() {
			checked = ", checked " + s.CheckedAt.UTC().Format(timeFormat)
		}
		fmt.Fprintf(w, "set-up (%s mode%s):\n", s.Checks.Mode, checked)
		PrintSteps(w, s.Checks)
		fmt.Fprintln(w, Verdict(s.Checks))
	case s.CheckError != "":
		fmt.Fprintf(w, "set-up: not checked: %s\n", s.CheckError)
	default:
		fmt.Fprintln(w, "set-up: not checked")
	}
	if run := s.LastRun; run != nil {
		line := fmt.Sprintf("last run: %s at %s", run.RunURL, run.Timestamp.UTC().Format(timeFormat))
		if c := run.Change; c != nil {
			line += " (" + c.Kind
			if c.By != "" {
				line += " by " + c.By
			}
			if c.PullRequest != nil {
				line += ", " + c.PullRequest.URL
			}
			line += ")"
		}
		fmt.Fprintln(w, line)
	}
	PrintPendingRun(w, s.PendingRun)
	if m := s.MissingRun; m != nil {
		where := m.RunsURL
		if m.RunURL != "" {
			where = m.RunURL + " (" + m.Conclusion + ")"
		}
		fmt.Fprintf(w, "missing run: the run expected since %s by %s never reported: %s\n", m.DispatchedAt.UTC().Format(timeFormat), m.By, where)
	}
	PrintFindings(w, r.Findings)
}

func yesNoUnknown(b *bool) string {
	switch {
	case b == nil:
		return "unknown"
	case *b:
		return "yes"
	default:
		return "no"
	}
}

func presence(present bool) string {
	if present {
		return "present"
	}
	return "missing"
}
