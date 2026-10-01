// Package validate constrains the caller-supplied strings that devctl
// interpolates into filesystem paths, subprocess arguments and URLs.
package validate

import (
	"fmt"
	"regexp"
)

// nameRegexp matches a lowercase DNS-style name: the shape of an app, team,
// provider, chart or cluster identifier. It admits no separator that gives a
// path segment, a URL segment or a leading dash that a subprocess would read as
// a flag.
var nameRegexp = regexp.MustCompile(`^[a-z0-9]([a-z0-9.]|-(?:-*[a-z0-9.]))*$`)

// Name reports whether value is a well-formed identifier. kind names the input
// in the error, for example "--team".
func Name(kind, value string) error {
	if value == "" {
		return fmt.Errorf("%s must not be empty", kind)
	}
	if len(value) > 253 {
		return fmt.Errorf("%s must be at most 253 characters, got %d", kind, len(value))
	}
	if !nameRegexp.MatchString(value) {
		return fmt.Errorf("%s must be lowercase alphanumeric, optionally separated by %q or %q, and must start and end with an alphanumeric character, got %q", kind, "-", ".", value)
	}

	return nil
}

// versionRegexp matches a semantic version, with or without a leading "v", and
// admits no character that changes the meaning of a URL path or a shell-free
// argument list.
var versionRegexp = regexp.MustCompile(`^v?[0-9]+(\.[0-9]+)*(-[a-zA-Z0-9.]+)?(\+[a-zA-Z0-9.]+)?$`)

// Version reports whether value is a well-formed version string.
func Version(kind, value string) error {
	if value == "" {
		return fmt.Errorf("%s must not be empty", kind)
	}
	if !versionRegexp.MatchString(value) {
		return fmt.Errorf("%s must be a version such as %q or %q, got %q", kind, "1.2.3", "v1.2.3-rc1", value)
	}

	return nil
}
