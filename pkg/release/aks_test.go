package release

import (
	"testing"

	"github.com/blang/semver"
)

// Lines as they appear in the release notes of Azure/AKS.
const aksReleaseNotes = `
#### Kubernetes versions

* Kubernetes version [1.37](https://github.com/kubernetes/kubernetes/blob/master/CHANGELOG/CHANGELOG-1.37.md) is now generally available. Kubernetes 1.36 is now the default version for new clusters.
* Kubernetes patch versions [1.37.1](https://github.com/kubernetes/kubernetes/blob/master/CHANGELOG/CHANGELOG-1.37.md), [1.36.5](https://github.com/kubernetes/kubernetes/blob/master/CHANGELOG/CHANGELOG-1.36.md), and [1.34.12](https://github.com/kubernetes/kubernetes/blob/master/CHANGELOG/CHANGELOG-1.34.md) are now available.
* kube-apiserver and kube-proxy images have been updated with new AKS builds across Kubernetes 1.31 to 1.37 patch versions.
* Cloud provider Azure has been updated to ` + "`v1.36.8-1`" + ` on AKS 1.36.
* AKS LTS (Long Term Support) patch versions are now available:
  * 1.30.100

* Kubernetes patch versions ` + "`1.36.3`, `1.35.7`, and `1.34.10`" + ` are now available.
* New Kubernetes patch versions are now available: ` + "`1.35.2`, `1.35.3`" + `.
* AKS Kubernetes patch versions 1.36.2, 1.33.13 are now available.
`

func TestParseAKSKubernetesVersions(t *testing.T) {
	want := []string{"1.37.1", "1.36.5", "1.34.12", "1.36.3", "1.35.7", "1.34.10", "1.35.2", "1.35.3", "1.36.2", "1.33.13"}

	got := parseAKSKubernetesVersions(aksReleaseNotes)
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i].String() != want[i] {
			t.Errorf("version %d: got %s, want %s", i, got[i], want[i])
		}
	}
}

func TestLatestAKSKubernetesVersion(t *testing.T) {
	versions := parseAKSKubernetesVersions(aksReleaseNotes)

	tests := []struct {
		name     string
		versions []semver.Version
		minor    uint64
		want     string
		wantErr  func(error) bool
	}{
		{name: "highest patch across release notes", versions: versions, minor: 36, want: "1.36.5"},
		{name: "older release notes list lower patches", versions: versions, minor: 35, want: "1.35.7"},
		{name: "minor not offered yet", versions: versions, minor: 38, wantErr: IsKubernetesVersionNotOffered},
		{name: "nothing parsed", versions: nil, minor: 36, wantErr: IsReleaseNotFound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := latestAKSKubernetesVersion(tc.versions, tc.minor)
			if tc.wantErr != nil {
				if !tc.wantErr(err) {
					t.Fatalf("got %q and unexpected error %v", got, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}
