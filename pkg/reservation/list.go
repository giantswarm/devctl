package reservation

import (
	"os"
	"sort"
	"time"

	"github.com/giantswarm/microerror"
	"gopkg.in/yaml.v3"
)

// ListRequest identifies the cluster whose reservations to list.
type ListRequest struct {
	// RepoDir is the working tree of the GitOps repo holding Cluster.
	RepoDir string
	// Cluster is the management cluster name, e.g. "graveler".
	Cluster string
}

// Reservation is one reservation, active or expired, read back exactly as
// reservationEntry wrote it. Only GitOpsSource comes from the files instead.
type Reservation struct {
	// App is the chart the reservation moved. It is the ConfigMap entry's key.
	App string `json:"app"`
	// User is the GitHub login of the holder.
	User string `json:"user"`
	// Branch is the app repo branch the reservation follows.
	Branch string `json:"branch"`
	// PullRequest is the holder's pull request.
	PullRequest string `json:"pullRequest,omitempty"`
	// Scope is the reservation's scope, e.g. ScopeApp.
	Scope string `json:"scope"`
	// Bundle is the bundle whose HelmRelease carries the reservation, empty for a
	// reservation of an app deployed on its own.
	Bundle string `json:"bundle,omitempty"`
	// GitOpsSource is where the reservation lives, GitOpsSourceCollections or
	// GitOpsSourceExtras. It comes from the files: release, extend and reap
	// follow them, so the files win over the entry.
	GitOpsSource string `json:"gitopsSource"`
	// recordedGitOpsSource is the gitops-source of the entry, empty when the
	// entry has none. See StaleGitOpsSource.
	recordedGitOpsSource string
	// From and Until bound the reservation, in UTC. Until is the expiry.
	From  time.Time `json:"from"`
	Until time.Time `json:"until"`
}

// StaleGitOpsSource returns the gitops-source the entry records, and whether it
// disagrees with GitOpsSource, the one the files show. An entry that records
// none is not stale: an older devctl wrote it.
func (r Reservation) StaleGitOpsSource() (recorded string, stale bool) {
	return r.recordedGitOpsSource, r.recordedGitOpsSource != "" && r.recordedGitOpsSource != r.GitOpsSource
}

// Expired reports whether the reservation no longer holds at now. An expired
// reservation is not a lock: a later Reserve of the same app replaces it.
func (r Reservation) Expired(now time.Time) bool {
	return !r.Until.After(now)
}

func (r ListRequest) validate() error {
	switch {
	case r.RepoDir == "":
		return microerror.Maskf(invalidConfigError, "%T.RepoDir must not be empty", r)
	case r.Cluster == "":
		return microerror.Maskf(invalidConfigError, "%T.Cluster must not be empty", r)
	}

	return nil
}

// List returns every reservation recorded on the cluster, sorted by app, read
// straight from its ConfigMap, including one whose Until has already passed
// but the reaper has not swept yet: Reap and checkCollision both rely on
// seeing those too, and do their own "active as of now" filtering. A caller
// that wants only the active ones, such as the list command, must filter
// Until against its own idea of now. Like Release, List works on an existing
// checkout: no render, no clone.
func List(req ListRequest) ([]Reservation, error) {
	if err := req.validate(); err != nil {
		return nil, microerror.Mask(err)
	}
	if err := checkEnabled(req.RepoDir, req.Cluster); err != nil {
		return nil, microerror.Mask(err)
	}

	configMapPath := configMapFilePath(req.RepoDir, req.Cluster)
	b, err := os.ReadFile(configMapPath) //nolint:gosec // the path is built from the repo checkout and the cluster name
	if err != nil {
		return nil, microerror.Mask(err)
	}

	var configMap struct {
		Data map[string]string `yaml:"data"`
	}
	if err := yaml.Unmarshal(b, &configMap); err != nil {
		return nil, microerror.Maskf(invalidConfigError, "parsing %s: %v", configMapPath, err)
	}

	reservations := make([]Reservation, 0, len(configMap.Data))
	for app, entry := range configMap.Data {
		reservation, err := parseReservationEntry(app, entry)
		if err != nil {
			return nil, microerror.Maskf(invalidConfigError, "parsing the reservation entry of %q in %s: %v", app, configMapPath, err)
		}
		reservation.GitOpsSource = gitopsSourceOf(req.RepoDir, req.Cluster, app)
		reservations = append(reservations, reservation)
	}
	sort.Slice(reservations, func(i, j int) bool { return reservations[i].App < reservations[j].App })

	return reservations, nil
}

// parseReservationEntry reverses reservationEntry: the same fields, read back
// the same way. An entry from an older devctl has no gitops-source.
func parseReservationEntry(app, entry string) (Reservation, error) {
	var fields struct {
		User, Branch, PR, Scope, From, Until string
		GitOpsSource                         string `yaml:"gitops-source"`
		Bundle                               string
	}
	if err := yaml.Unmarshal([]byte(entry), &fields); err != nil {
		return Reservation{}, microerror.Mask(err)
	}

	from, err := time.Parse(time.RFC3339, fields.From)
	if err != nil {
		return Reservation{}, microerror.Maskf(invalidConfigError, "invalid `from`: %v", err)
	}
	until, err := time.Parse(time.RFC3339, fields.Until)
	if err != nil {
		return Reservation{}, microerror.Maskf(invalidConfigError, "invalid `until`: %v", err)
	}

	return Reservation{
		App:         app,
		User:        fields.User,
		Branch:      fields.Branch,
		PullRequest: fields.PR,
		Scope:       fields.Scope,
		Bundle:      fields.Bundle,
		From:        from,
		Until:       until,

		recordedGitOpsSource: fields.GitOpsSource,
	}, nil
}
