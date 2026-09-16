package reservation

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/giantswarm/gitsemver/v2/pkg/gitsemver"
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

	// minVersionBaseLen and maxVersionBaseLen bound the length of an "X.Y.Z"
	// version base. gitsemver spends what is left of the 63-character budget on
	// the branch, so the branch segment of a dev tag depends on how long the
	// base is, and the base is a property of the app repo that the GitOps repo
	// never records.
	// ponytail: 14 covers "9999.9999.9999"; widen it if an app ever ships a
	// longer version base.
	minVersionBaseLen = 5
	maxVersionBaseLen = 14
)

// devSemverFilter returns the semVer filter that selects every dev build of
// branch.
//
// The branch segment of a dev tag is the sanitized branch after a middle
// truncation whose budget is 63 minus the fixed parts, and the version base is
// one of those fixed parts. Nothing in the GitOps repo records the app's version
// base: collection source objects carry `semver: x.x.x`, which the stage
// rewrites to a range, so there is no version to read a base from. The filter
// therefore accepts the segment for every base length a real "X.Y.Z" can have,
// and every alternative comes from gitsemver itself.
func devSemverFilter(branch string) (string, error) {
	var segments []string
	seen := map[string]bool{}

	for n := minVersionBaseLen; n <= maxVersionBaseLen; n++ {
		// Only the length of the base moves the branch budget, so any valid
		// "X.Y.Z" string of that length will do.
		base := strings.Repeat("9", n-4) + ".9.9"

		segment, err := gitsemver.DevVersionBranch(branch, base, 0)
		if err != nil {
			return "", microerror.Maskf(invalidConfigError, "building a version filter for branch %q: %v", branch, err)
		}
		if !seen[segment] {
			seen[segment] = true
			segments = append(segments, regexp.QuoteMeta(segment))
		}
	}

	return `^[0-9]+\.[0-9]+\.[0-9]+-dev\.(` + strings.Join(segments, "|") + `)\..*$`, nil
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
