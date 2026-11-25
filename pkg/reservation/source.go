package reservation

import (
	"fmt"
	"strings"
	"time"

	"github.com/giantswarm/gitsemver/v3/pkg/gitsemver"
	"github.com/giantswarm/microerror"
)

const (
	// AnnotationPrefix prefixes the reservation annotations a reservation patches
	// onto the app's source objects, so `kubectl` can answer "why is this version
	// running".
	AnnotationPrefix = "reservation.giantswarm.io/"
	// SourceInterval is how often Flux re-checks a reserved source. A dev
	// build has to land quickly, unlike a release.
	SourceInterval = "1m"

	// Keys after AnnotationPrefix that extend rewrites as well as reserve.
	annotationUser  = "user"
	annotationFrom  = "from"
	annotationUntil = "until"

	// devSemverRange selects every version, including pre-releases. The filter
	// does the actual selecting.
	devSemverRange = ">=0.0.0-0"
)

// devSemverFilter returns the semVer filter that selects every dev build of
// branch.
//
// A dev tag carries the branch as a fixed-width CRC32 fingerprint, so the
// filter does not depend on the app's version base. It cannot: nothing in the
// GitOps repo records that base -- collection source objects carry
// `semver: x.x.x`, which the stage rewrites to a range.
//
// Every field is width-pinned. A loose tail would let the filter match an
// `-rc.N` tag, and a reservation that follows release candidates is worse than
// one that follows nothing, because it looks like it works.
func devSemverFilter(branch string) string {
	return `^[0-9]+\.[0-9]+\.[0-9]+-r` + gitsemver.BranchHash(branch) + `t[0-9]{14}h[0-9a-f]{7}$`
}

// devRef is the OCIRepository spec.ref that follows the dev builds semverFilter
// selects.
func devRef(semverFilter string) map[string]any {
	return map[string]any{"semver": devSemverRange, "semverFilter": semverFilter}
}

// hold is who holds a reservation and for which window: the part of the
// annotations that extend moves as well as reserve.
type hold struct {
	User        string
	From, Until time.Time
}

// annotations returns the hold's annotations, keyed without AnnotationPrefix.
func (h hold) annotations() map[string]string {
	return map[string]string{
		annotationUser:  h.User,
		annotationFrom:  h.From.Format(time.RFC3339),
		annotationUntil: h.Until.Format(time.RFC3339),
	}
}

// reservationAnnotations are the annotations a reserved source carries, so
// `kubectl get` on the source alone shows who holds it and until when.
func reservationAnnotations(req Request, from, until time.Time) map[string]string {
	keys := hold{User: req.User, From: from, Until: until}.annotations()
	keys["branch"] = req.Branch
	keys["pr"] = req.PullRequest
	keys["scope"] = req.Scope

	annotations := map[string]string{}
	for key, value := range keys {
		annotations[AnnotationPrefix+key] = value
	}

	return annotations
}

// The GitOps sources a reservation can live in: a block in the cluster's
// collections kustomization, or one in its extras kustomization.
const (
	GitOpsSourceCollections = "collections"
	GitOpsSourceExtras      = "extras"
)

// reservationEntry renders one reservation as the one-line YAML flow mapping the
// reservations ConfigMap stores, so `kubectl get cm reservations -o yaml` stays
// readable and a machine can still parse it.
func reservationEntry(chart string, req Request, gitopsSource string, from, until time.Time) (string, error) {
	fields := [][2]string{
		{annotationUser, req.User},
		{"branch", req.Branch},
		{"pr", req.PullRequest},
		{"scope", req.Scope},
		{"gitops-source", gitopsSource},
		{annotationFrom, from.Format(time.RFC3339)},
		{annotationUntil, until.Format(time.RFC3339)},
	}

	if req.Bundle != "" {
		fields = append(fields[:5], append([][2]string{{"bundle", req.Bundle}}, fields[5:]...)...)
	}

	flow, err := flowMapping(fields)
	if err != nil {
		return "", microerror.Mask(err)
	}

	// A single-quoted YAML scalar, so a branch name carrying a comma or a brace
	// cannot break the ConfigMap for every other reservation on the cluster.
	return fmt.Sprintf("%s: '%s'", chart, strings.ReplaceAll(flow, "'", "''")), nil
}
