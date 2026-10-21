package reservation

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/giantswarm/microerror"
	"gopkg.in/yaml.v3"
)

// MaxDurationAnnotation is the annotation a cluster's owners put on the
// reservations ConfigMap to lower the maximum reservation length below
// MaxDuration.
const MaxDurationAnnotation = "reservations.giantswarm.io/max-duration"

// dayForm matches a whole number of days, the form people reach for for a
// multi-day reservation. time.ParseDuration has no day unit, so it takes every
// other form.
var dayForm = regexp.MustCompile(`^([0-9]+)d$`)

// AcceptedDurations is the list every duration refusal carries, so the fix is
// in the message and nobody has to go and read documentation for it.
const AcceptedDurations = "30m, 4h, 4h30m or 2d: a positive whole number of minutes in Go duration form (m, h), or a whole number of days (d)"

// clampedDuration returns how long the requested reservation may last, or
// refuses one that runs past the maximum the cluster allows. An unrequested
// duration (zero) is the default, held to that maximum.
func clampedDuration(req Request, configMapPath string) (time.Duration, error) {
	maxDuration, err := clusterMaxDuration(configMapPath)
	if err != nil {
		return 0, microerror.Mask(err)
	}

	// Nobody asked for the default, so a cluster whose maximum is below it
	// takes the maximum instead of refusing a length nobody typed.
	duration := req.Duration
	if duration == 0 {
		duration = min(DefaultDuration, maxDuration)
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
			"the %s annotation in %s is %q, which is not a duration. Accepted forms are %s", MaxDurationAnnotation, path, value, AcceptedDurations)
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
		// Whole minutes, so the String form always ends in a zero seconds part.
		return strings.TrimSuffix(d.String(), "0s")
	}
}

// ParseDuration reads a reservation duration: a whole number of days, or a
// time.ParseDuration string that is positive and a whole number of minutes,
// so "1ns" and "-4h" are not reservations. An empty string is the default, so
// a caller never has to special-case a duration nobody gave.
func ParseDuration(s string) (time.Duration, error) {
	if s == "" {
		return DefaultDuration, nil
	}

	if m := dayForm.FindStringSubmatch(s); m != nil {
		// The pattern already limits this to digits, so only an overflow fails.
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			day := 24 * time.Hour
			// A number big enough to overflow the nanosecond counter wraps round
			// into a short duration, which would then walk straight past the
			// maximum. Dividing back catches it.
			if d := time.Duration(n) * day; d > 0 && d/day == time.Duration(n) {
				return d, nil
			}
		}
	} else if d, err := time.ParseDuration(s); err == nil && d > 0 && d%time.Minute == 0 {
		return d, nil
	}

	return 0, microerror.Maskf(invalidDurationError,
		"%q is not a duration. Accepted forms are %s", s, AcceptedDurations)
}
