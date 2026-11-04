package list_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

const sourceConfigMap = "data:\n" +
	"  hello-world: '{user: alice, branch: fix/crash, pr: \"\", scope: app, from: 2026-09-15T10:00:00Z, until: 2026-09-15T20:00:00Z}'\n"

func TestListRefusesUntrackedReservationConfigMap(t *testing.T) {
	const cluster = "graveler"
	const path = "management-clusters/" + cluster + "/configmap-reservations.yaml"
	for _, output := range []string{"table", "json"} {
		for _, ignored := range []bool{false, true} {
			name := output + "/untracked"
			if ignored {
				name = output + "/ignored"
			}
			t.Run(name, func(t *testing.T) {
				files := map[string]string{
					"management-clusters/" + cluster + "/kustomization.yaml": "resources: []\n",
				}
				if ignored {
					files[".gitignore"] = path + "\n"
				}
				dir := newCheckout(t, files)
				localPath := filepath.Join(dir, path)
				if err := os.WriteFile(localPath, []byte(sourceConfigMap), 0o600); err != nil {
					t.Fatal(err)
				}

				var stdout bytes.Buffer
				cmd := newCommand(t, &stdout)
				cmd.SetArgs([]string{repoDirFlag, dir, "--cluster", cluster, "--output", output})
				err := cmd.Execute()
				if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "synced main") {
					t.Fatalf("got %v, want a refusal naming the state path and synced main", err)
				}
				if stdout.Len() != 0 {
					t.Errorf("local-only reservations were printed: %q", stdout.String())
				}
				assertSourceFile(t, localPath, sourceConfigMap)
			})
		}
	}
}

func TestListLeavesUnrelatedUntrackedFilesAlone(t *testing.T) {
	const cluster = "graveler"
	for _, output := range []string{"table", "json"} {
		t.Run(output, func(t *testing.T) {
			dir := newCheckout(t, map[string]string{
				"management-clusters/" + cluster + "/configmap-reservations.yaml": sourceConfigMap,
			})
			path := filepath.Join(dir, "notes.txt")
			const notes = "local notes must survive\n"
			if err := os.WriteFile(path, []byte(notes), 0o600); err != nil {
				t.Fatal(err)
			}

			var stdout bytes.Buffer
			cmd := newCommand(t, &stdout)
			cmd.SetArgs([]string{repoDirFlag, dir, "--cluster", cluster, "--output", output})
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(stdout.String(), "hello-world") {
				t.Errorf("committed reservation is missing from %q", stdout.String())
			}
			assertSourceFile(t, path, notes)
		})
	}
}

func TestListPreservesMissingStateErrors(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		isErr func(error) bool
	}{
		{"disabled cluster", map[string]string{"management-clusters/graveler/kustomization.yaml": "resources: []\n"}, reservation.IsClusterNotEnabled},
		{"missing cluster", map[string]string{"management-clusters/other/kustomization.yaml": "resources: []\n"}, reservation.IsClusterNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := newCheckout(t, tc.files)
			var stdout bytes.Buffer
			cmd := newCommand(t, &stdout)
			cmd.SetArgs([]string{repoDirFlag, dir, "--cluster", "graveler"})
			if err := cmd.Execute(); !tc.isErr(err) {
				t.Fatalf("unexpected missing-state error: %v", err)
			}
			if stdout.Len() != 0 {
				t.Errorf("failed list wrote %q", stdout.String())
			}
		})
	}
}

func assertSourceFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path) //nolint:gosec // test-created path under t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("local file changed: got %q, want %q", got, want)
	}
}
