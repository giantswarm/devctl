package reservation

import (
	"os"
	"path/filepath"
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

// Reservation is one active reservation, read back exactly as
// reservationEntry wrote it: never reconstructed from anything else.
type Reservation struct {
	// App is the chart the reservation moved. It is the ConfigMap entry's key.
	App string
	// User is the GitHub login of the holder.
	User string
	// Branch is the app repo branch the reservation follows.
	Branch string
	// PullRequest is the holder's pull request.
	PullRequest string
	// Scope is the reservation's scope, e.g. ScopeApp.
	Scope string
	// From and Until bound the reservation, in UTC. Until is the expiry.
	From, Until time.Time
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

	configMapPath := filepath.Join(req.RepoDir, clustersDir, req.Cluster, ConfigMapFile)
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
		reservations = append(reservations, reservation)
	}
	sort.Slice(reservations, func(i, j int) bool { return reservations[i].App < reservations[j].App })

	return reservations, nil
}

// parseReservationEntry reverses reservationEntry: the same six fields, read
// back the same way.
func parseReservationEntry(app, entry string) (Reservation, error) {
	var fields struct {
		User, Branch, PR, Scope, From, Until string
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
		From:        from,
		Until:       until,
	}, nil
}
