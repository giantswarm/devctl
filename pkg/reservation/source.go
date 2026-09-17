package reservation

import (
	"fmt"
	"strings"
	"time"

	"github.com/giantswarm/gitsemver/v3/pkg/gitsemver"
	"github.com/giantswarm/microerror"
	"github.com/mohae/deepcopy"
)

const (
	// SourceNameSuffix is appended to the app name to name the reservation's own
	// source object. The app's own source object is never touched.
	SourceNameSuffix = "-dev-reservation"
	// AnnotationPrefix prefixes the reservation annotations stamped on the new
	// source object, so `kubectl` can answer "why is this version running".
	AnnotationPrefix = "reservation.giantswarm.io/"
	// SourceInterval is how often Flux re-checks the reservation source. A dev
	// build has to land quickly, unlike a release.
	SourceInterval = "1m"

	// devSemverRange selects every version, including pre-releases. The filter
	// does the actual selecting.
	devSemverRange = ">=0.0.0-0"
)

// devSemverFilter returns the semVer filter that selects every dev build of
// branch.
//
// A dev tag carries the branch as a fixed-width CRC32 fingerprint
// (gitsemver v3), so the filter no longer depends on the app's version base.
// It cannot: nothing in the GitOps repo records that base -- collection source
// objects carry `semver: x.x.x`, which the stage rewrites to a range.
//
// Every field is width-pinned. A loose tail would let the filter match an
// `-rc.N` tag, and a reservation that follows release candidates is worse than
// one that follows nothing, because it looks like it works.
func devSemverFilter(branch string) string {
	return `^[0-9]+\.[0-9]+\.[0-9]+-r` + gitsemver.BranchHash(branch) + `t[0-9]{14}h[0-9a-f]{7}$`
}

// devSource builds the reservation's source object as a copy of the app's
// resolved one, changing only the name, the annotations and the version
// selector, plus the interval.
//
// A hand-built object silently loses secretRef, provider, verify, insecure and
// layerSelector. A lost secretRef is a registry authentication failure that
// looks exactly like a missing chart.
func devSource(original object, sourceName string, req Request, semverFilter string, from, until time.Time) object {
	source, _ := deepcopy.Copy(map[string]any(original)).(map[string]any)
	o := object(source)

	metadata := o.metadata()
	metadata["name"] = sourceName

	annotations, _ := metadata["annotations"].(map[string]any)
	if annotations == nil {
		annotations = map[string]any{}
		metadata["annotations"] = annotations
	}
	for key, value := range map[string]string{
		"user":   req.User,
		"branch": req.Branch,
		"pr":     req.PullRequest,
		"scope":  req.Scope,
		"from":   from.Format(time.RFC3339),
		"until":  until.Format(time.RFC3339),
	} {
		annotations[AnnotationPrefix+key] = value
	}

	spec, _ := o["spec"].(map[string]any)
	spec["interval"] = SourceInterval
	spec["ref"] = map[string]any{
		"semver":       devSemverRange,
		"semverFilter": semverFilter,
	}

	return o
}

// reservationEntry renders one reservation as the one-line YAML flow mapping the
// reservations ConfigMap stores, so `kubectl get cm reservations -o yaml` stays
// readable and a machine can still parse it.
func reservationEntry(chart string, req Request, from, until time.Time) (string, error) {
	fields := [][2]string{
		{"user", req.User},
		{"branch", req.Branch},
		{"pr", req.PullRequest},
		{"scope", req.Scope},
		{"from", from.Format(time.RFC3339)},
		{"until", until.Format(time.RFC3339)},
	}

	flow, err := flowMapping(fields)
	if err != nil {
		return "", microerror.Mask(err)
	}

	// A single-quoted YAML scalar, so a branch name carrying a comma or a brace
	// cannot break the ConfigMap for every other reservation on the cluster.
	return fmt.Sprintf("%s: '%s'", chart, strings.ReplaceAll(flow, "'", "''")), nil
}
