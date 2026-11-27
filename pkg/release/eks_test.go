package release

import (
	"slices"
	"testing"
)

// Headings as they appear in the Amazon EKS user guide.
const eksVersionsPage = `
= Review release notes for Kubernetes versions on standard support

[#kubernetes-1-37]
== Kubernetes 1.37

Kubernetes ` + "`1.37`" + ` is now available in Amazon EKS. Before upgrading to 1.38, audit your clusters.

[#kubernetes-1-36]
== Kubernetes 1.36

=== Anonymous authentication changes in Kubernetes 1.30
`

func TestParseEKSKubernetesMinors(t *testing.T) {
	got := parseEKSKubernetesMinors(eksVersionsPage)
	want := []uint64{37, 36}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCheckEKSOffersKubernetesMinor(t *testing.T) {
	minors := parseEKSKubernetesMinors(eksVersionsPage)

	tests := []struct {
		name    string
		minors  []uint64
		minor   uint64
		wantErr bool
	}{
		{name: "offered", minors: minors, minor: 37},
		{name: "not offered yet", minors: minors, minor: 38, wantErr: true},
		{name: "nothing parsed", minors: nil, minor: 37, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := checkEKSOffersKubernetesMinor(tc.minors, tc.minor)
			if tc.wantErr != IsReleaseNotFound(err) {
				t.Errorf("got error %v, want error: %t", err, tc.wantErr)
			}
		})
	}
}
