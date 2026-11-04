package reservation_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

func TestReleaseRefusesSymlinkedComponent(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})
	if _, err := reservation.Reserve(testRequest(dir)); err != nil {
		t.Fatal(err)
	}
	component := filepath.Join(dir, "management-clusters", fixtureCluster, "collections", "reservations", fixtureApp)
	kept := component + "-kept"
	if err := os.Rename(component, kept); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(kept, component); err != nil {
		t.Fatal(err)
	}
	_, err := reservation.Release(testReleaseRequest(dir))
	if err == nil || !strings.Contains(err.Error(), "non-directory") {
		t.Fatalf("got %v, want refusal before following symlink", err)
	}
	if info, err := os.Lstat(component); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("component symlink was changed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(kept, "kustomization.yaml")); err != nil {
		t.Errorf("symlink target was changed: %v", err)
	}
}
