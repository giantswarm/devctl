package reservation

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/giantswarm/microerror"
	"helm.sh/helm/v4/pkg/chart/common"
	"helm.sh/helm/v4/pkg/chart/common/util"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/engine"
	"helm.sh/helm/v4/pkg/registry"
)

// BundleRegistry reads bundle charts from an OCI registry. ref is the chart's
// path in the registry, without the oci:// scheme.
type BundleRegistry interface {
	// Tags lists the versions of the chart.
	Tags(ref string) ([]string, error)
	// Pull loads the chart at tag.
	Pull(ref, tag string) (*chart.Chart, error)
}

// helmRegistry is the BundleRegistry of the real OCI registry.
type helmRegistry struct{ client *registry.Client }

func newHelmRegistry() (BundleRegistry, error) {
	c, err := registry.NewClient()
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return helmRegistry{c}, nil
}

func (h helmRegistry) Tags(ref string) ([]string, error) { return h.client.Tags(ref) }

func (h helmRegistry) Pull(ref, tag string) (*chart.Chart, error) {
	res, err := h.client.Pull(ref + ":" + tag)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	c, err := loader.LoadArchive(bytes.NewReader(res.Chart.Data))

	return c, microerror.Mask(err)
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

	reg := req.BundleRegistry
	if reg == nil {
		var err error
		if reg, err = newHelmRegistry(); err != nil {
			return microerror.Mask(err)
		}
	}
	tag, err := bundleTag(reg, ref, source)
	if err != nil {
		return microerror.Mask(err)
	}
	c, err := reg.Pull(ref, tag)
	if err != nil {
		return microerror.Maskf(invalidConfigError, "pulling the bundle %s:%s: %v", ref, tag, err)
	}

	return microerror.Mask(checkBundleMember(c, req.Bundle, tag, memberChart))
}

// bundleTag is the newest tag the ref of source allows: its tag, or the highest
// version of its semver range.
func bundleTag(reg BundleRegistry, ref string, source object) (string, error) {
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
	tags, err := reg.Tags(ref)
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
		os, err := decodeObjects([]byte(files[n]))
		if err != nil {
			return microerror.Maskf(invalidConfigError, "bundle %s: %s: %v", at, n, err)
		}
		objects = append(objects, os...)
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
