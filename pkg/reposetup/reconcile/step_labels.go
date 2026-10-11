package reconcile

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/go-github/v92/github"
)

// DefaultLabelColor is the colour of a label the labels step creates when
// nothing declares one: GitHub's own for a label it creates on first use,
// the colour most team/<team> labels of the organization carry.
const DefaultLabelColor = "ededed"

// teamFilePrefix is the prefix of a team's file name that its label drops:
// team-bumblebee files issues under team/bumblebee.
const teamFilePrefix = "team-"

// Label is one issue label the labels step keeps on a repository: created
// when missing, never changed or removed once there.
type Label struct {
	// Name is the label's name; GitHub compares it case-insensitively.
	Name string `json:"name"`
	// Color is the six hex digits GitHub takes, without '#'; empty means
	// [DefaultLabelColor].
	Color string `json:"color,omitempty"`
	// Description is the label's description; empty leaves it without one.
	Description string `json:"description,omitempty"`
}

// TeamLabel is the label a team's issues carry across the organization's
// repositories: team/<team> for the team file team-<team>, the label the
// issue automation applies on assignment.
func TeamLabel(team string) string {
	return "team/" + strings.TrimPrefix(team, teamFilePrefix)
}

// labels are the labels the run keeps on the repository: the owning team's.
// A label the entry declares is appended here.
func (s *run) labels() []Label {
	return []Label{{Name: TeamLabel(s.req.Team)}}
}

// stepLabels creates every label the run keeps that the repository lacks.
// A label that exists is left as it is, whatever its colour and
// description: a team may have chosen them, and an issue keeps its label
// by name.
func (r *Runner) stepLabels(ctx context.Context, s *run, sr *StepResult) error {
	var present []string
	for _, l := range s.labels() {
		// go-github puts the name into the path as it is; escaped, team/<team>
		// is the one path segment the API documents.
		_, resp, err := r.GitHub.Issues.GetLabel(ctx, s.owner, s.name, url.PathEscape(l.Name))
		switch {
		case isNotFound(resp, err):
		case err != nil:
			return err
		default:
			present = append(present, l.Name)
			continue
		}
		color := l.Color
		if color == "" {
			color = DefaultLabelColor
		}
		err = s.plan(sr, fmt.Sprintf("create the label %s (#%s)", l.Name, color), func() error {
			label := github.CreateIssueLabelRequest{Name: l.Name, Color: new(color)}
			if l.Description != "" {
				label.Description = new(l.Description)
			}
			_, _, err := r.GitHub.Issues.CreateLabel(ctx, s.owner, s.name, label)
			return err
		})
		if err != nil {
			return err
		}
	}
	if len(sr.Changes) == 0 {
		sr.Summary = "has " + strings.Join(present, ", ")
	}
	return nil
}
