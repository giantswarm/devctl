package reservation

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/giantswarm/microerror"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"helm.sh/helm/v4/pkg/chart/common"
	"helm.sh/helm/v4/pkg/chart/common/util"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/engine"
)

// helmChartLayer is the media type of the layer that holds a chart in an OCI
// registry.
const helmChartLayer types.MediaType = "application/vnd.cncf.helm.chart.content.v1.tar+gzip"

// bundleRegistryTimeout bounds each read of the bundle registry, so that a
// registry that accepts the connection and stalls cannot hold a CI job. It is a
// variable for the test.
var bundleRegistryTimeout = 60 * time.Second

// ociTag and fromOCITag map a version to its OCI tag and back: a tag cannot
// hold `+`, so helm and Flux store it as `_`.
func ociTag(version string) string { return strings.ReplaceAll(version, "+", "_") }
func fromOCITag(tag string) string { return strings.ReplaceAll(tag, "_", "+") }

// bundleTags lists the versions of the chart at ref, the chart's path in the
// registry without the oci:// scheme. Like the other engines it reads no
// credential store: the registry is read anonymously.
func bundleTags(ref string) ([]string, error) {
	repo, err := name.NewRepository(ref)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), bundleRegistryTimeout)
	defer cancel()
	tags, err := remote.List(repo, remote.WithAuth(authn.Anonymous), remote.WithContext(ctx))
	for i, t := range tags {
		tags[i] = fromOCITag(t)
	}

	return tags, microerror.Mask(err)
}

// pullBundle loads the chart at ref and tag, anonymously.
func pullBundle(ref, tag string) (*chart.Chart, error) {
	tagRef, err := name.NewTag(ref + ":" + ociTag(tag))
	if err != nil {
		return nil, microerror.Mask(err)
	}
	// The context also bounds the layer read below.
	ctx, cancel := context.WithTimeout(context.Background(), bundleRegistryTimeout)
	defer cancel()
	img, err := remote.Image(tagRef, remote.WithAuth(authn.Anonymous), remote.WithContext(ctx))
	if err != nil {
		return nil, microerror.Mask(err)
	}
	layers, err := img.Layers()
	if err != nil {
		return nil, microerror.Mask(err)
	}
	for _, l := range layers {
		if mt, err := l.MediaType(); err != nil || mt != helmChartLayer {
			continue
		}
		r, err := l.Compressed()
		if err != nil {
			return nil, microerror.Mask(err)
		}
		defer func() { _ = r.Close() }()
		c, err := loader.LoadArchive(r)

		return c, microerror.Mask(err)
	}

	return nil, microerror.Maskf(invalidConfigError, "%s:%s has no Helm chart layer", ref, tag)
}

// checkBundleChart is the bundle check: a kustomize patch whose target matches
// nothing is skipped without an error, and the bot has no cluster access, so a
// reservation of a bundle that cannot honor it must be refused here. It pulls
// the bundle chart at the newest version that the ref of the bundle's source
// allows, and refuses a chart that renders App CRs, that has no member of
// chart, or whose member OCIRepository has no member label.
//
// Flux can pick a newer bundle version after the reservation, and that version
// can drop the member. Nothing here can see that.
func checkBundleChart(req Request, memberChart string, resolved blockResolution) error {
	if req.Bundle == "" {
		return nil
	}
	if len(resolved.sources) == 0 {
		return microerror.Maskf(appNotFoundError, "the bundle %q resolved to no %s", req.Bundle, ociRepositoryKind)
	}
	source := resolved.sources[0]
	url, _ := source.nested("spec", "url").(string)
	ref := strings.TrimPrefix(url, "oci://")

	tag, err := bundleTag(req.BundleTags, ref, source)
	if err != nil {
		return microerror.Mask(err)
	}
	c, err := req.BundleChart(ref, tag)
	if err != nil {
		return microerror.Maskf(invalidConfigError, "pulling the bundle %s:%s: %v", ref, tag, err)
	}

	return microerror.Mask(checkBundleMember(c, req.Bundle, tag, memberChart))
}

// bundleTag is the newest tag the ref of source allows: its tag, or the highest
// version of its semver range.
func bundleTag(list func(ref string) ([]string, error), ref string, source object) (string, error) {
	if tag, _ := source.nested("spec", "ref", "tag").(string); tag != "" {
		return tag, nil
	}
	rng, _ := source.nested("spec", "ref", "semver").(string)
	if rng == "" {
		return "", microerror.Maskf(appNotSupportedError, "%s %s has neither spec.ref.tag nor spec.ref.semver, so devctl cannot tell which bundle version Flux deploys", ociRepositoryKind, source.name())
	}
	constraint, err := semver.NewConstraint(rng)
	if err != nil {
		return "", microerror.Maskf(appNotSupportedError, "spec.ref.semver %q of %s %s is not a semver range: %v", rng, ociRepositoryKind, source.name(), err)
	}
	tags, err := list(ref)
	if err != nil {
		return "", microerror.Maskf(invalidConfigError, "listing the versions of %s: %v", ref, err)
	}
	var best *semver.Version
	bestTag := ""
	for _, t := range tags {
		if v, err := semver.NewVersion(t); err == nil && constraint.Check(v) && (best == nil || v.GreaterThan(best)) {
			best, bestTag = v, t
		}
	}
	if best == nil {
		return "", microerror.Maskf(invalidConfigError, "no version of %s satisfies the semver range %q of %s %s", ref, rng, ociRepositoryKind, source.name())
	}

	return bestTag, nil
}

// checkBundleMember renders c with its default values and checks it for a member
// of memberChart.
func checkBundleMember(c *chart.Chart, bundle, tag, memberChart string) error {
	at := fmt.Sprintf("%s %s", bundle, tag)
	vals, err := util.ToRenderValues(c, nil, common.ReleaseOptions{Name: bundle, Namespace: Namespace, IsInstall: true}, nil)
	if err != nil {
		return microerror.Maskf(invalidConfigError, "the values of bundle %s: %v", at, err)
	}
	files, err := engine.Render(c, vals)
	if err != nil {
		return microerror.Maskf(invalidConfigError, "rendering bundle %s: %v", at, err)
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var objects []object
	for _, n := range names {
		if !strings.HasSuffix(n, ".yaml") && !strings.HasSuffix(n, ".yml") {
			continue
		}
		decoded, err := decodeObjects([]byte(files[n]))
		if err != nil {
			return microerror.Maskf(invalidConfigError, "bundle %s: %s: %v", at, n, err)
		}
		objects = append(objects, decoded...)
	}

	for _, o := range objects {
		if api, _ := o["apiVersion"].(string); o.kind() == "App" && strings.HasPrefix(api, "application.giantswarm.io/") {
			return microerror.Maskf(appNotSupportedError,
				"bundle %s renders App CRs, and only a Flux-style bundle (OCIRepository and HelmRelease per member) can be reserved. Use a bundle version that is Flux-style", at)
		}
	}

	apps, _ := c.Values["apps"].(map[string]any)
	var charts []string
	member := false
	for _, a := range apps {
		name, _ := a.(map[string]any)["chartName"].(string)
		charts = append(charts, name)
		member = member || name == memberChart
	}
	if !member {
		sort.Strings(charts)
		return microerror.Maskf(appNotFoundError,
			"chart %q is not a member of bundle %s: no apps.<key>.chartName in its values.yaml is %q (it lists %s). Check --app and --bundle",
			memberChart, at, memberChart, strings.Join(charts, ", "))
	}

	for _, o := range objects {
		labels, _ := o.metadata()["labels"].(map[string]any)
		if o.kind() == ociRepositoryKind && labels[memberChartLabel] == memberChart {
			return nil
		}
	}

	return microerror.Maskf(appNotSupportedError,
		"no member %s of bundle %s carries the label %s: %s, so the reservation could not select it and the stable build would keep running. The bundle must add the label to each member %s",
		ociRepositoryKind, at, memberChartLabel, memberChart, ociRepositoryKind)
}
