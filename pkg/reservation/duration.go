package reservation

import (
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/giantswarm/microerror"
	"gopkg.in/yaml.v3"
)

// MaxDurationAnnotation is the annotation a cluster's owners put on the
// reservations ConfigMap to lower the maximum reservation length below
// MaxDuration.
const MaxDurationAnnotation = "reservations.giantswarm.io/max-duration"

// durationForm matches the whole accepted grammar: a positive whole number of
// minutes, hours or days.
//
// Not time.ParseDuration: it rejects "2d", which is the form people reach for
// for a multi-day reservation, and it accepts "1ns" and "-4h", which are not
// reservations anybody means.
var durationForm = regexp.MustCompile(`^([0-9]+)([mhd])$`)

// durationUnits maps the accepted suffixes.
var durationUnits = map[string]time.Duration{
	"m": time.Minute,
	"h": time.Hour,
	"d": 24 * time.Hour,
}

// acceptedDurations is the list every duration refusal carries, so the fix is
// in the message and nobody has to go and read documentation for it.
const acceptedDurations = "30m, 4h, 2d"

// clampedDuration returns how long the requested reservation may last, or
// refuses one that runs past the maximum the cluster allows.
func clampedDuration(req Request, configMapPath string) (time.Duration, error) {
	duration := req.Duration
	if duration == 0 {
		duration = DefaultDuration
	}

	maxDuration, err := clusterMaxDuration(configMapPath)
	if err != nil {
		return 0, microerror.Mask(err)
	}
	if duration > maxDuration {
		return 0, microerror.Maskf(invalidDurationError,
			"a reservation of %s is longer than the maximum of %s on %s",
			formatDuration(duration), formatDuration(maxDuration), req.Cluster)
	}

	return duration, nil
}

// clusterMaxDuration reads the cluster's own limit off the reservations
// ConfigMap, capped at MaxDuration: the annotation is how a cluster's owners
// lower the limit, never how they raise it.
//
// A malformed annotation is a refusal, not a fall back to MaxDuration. The
// owners set it to keep reservations short, and silently ignoring a typo would
// hand out the longest reservation instead of the shortest.
func clusterMaxDuration(path string) (time.Duration, error) {
	b, err := os.ReadFile(path) //nolint:gosec // the path is built from the repo checkout and the cluster name
	if err != nil {
		return 0, microerror.Mask(err)
	}

	var configMap struct {
		Metadata struct {
			Annotations map[string]string `yaml:"annotations"`
		} `yaml:"metadata"`
	}
	if err := yaml.Unmarshal(b, &configMap); err != nil {
		return 0, microerror.Maskf(invalidConfigError, "parsing %s: %v", path, err)
	}

	value := configMap.Metadata.Annotations[MaxDurationAnnotation]
	if value == "" {
		return MaxDuration, nil
	}

	clusterMax, err := ParseDuration(value)
	if err != nil {
		return 0, microerror.Maskf(invalidDurationError,
			"the %s annotation in %s is %q, which is not a duration. Accepted forms are %s", MaxDurationAnnotation, path, value, acceptedDurations)
	}

	return min(clusterMax, MaxDuration), nil
}

// formatDuration renders a duration in the form a caller may type back. It
// never sees a duration ParseDuration did not produce.
func formatDuration(d time.Duration) string {
	switch {
	case d%(24*time.Hour) == 0:
		return strconv.Itoa(int(d/(24*time.Hour))) + "d"
	case d%time.Hour == 0:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	default:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	}
}

// ParseDuration reads a reservation duration. An empty string is the default,
// so a caller never has to special-case a duration nobody gave.
func ParseDuration(s string) (time.Duration, error) {
	if s == "" {
		return DefaultDuration, nil
	}

	if m := durationForm.FindStringSubmatch(s); m != nil {
		// The pattern already limits this to digits, so only an overflow fails.
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			unit := durationUnits[m[2]]
			// A number big enough to overflow the nanosecond counter wraps round
			// into a short duration, which would then walk straight past the
			// maximum. Dividing back catches it.
			if d := time.Duration(n) * unit; d > 0 && d/unit == time.Duration(n) {
				return d, nil
			}
		}
	}

	return 0, microerror.Maskf(invalidDurationError,
		"%q is not a duration. Accepted forms are %s: a whole number of minutes (m), hours (h) or days (d)",
		s, acceptedDurations)
}
