package reservation_test

import (
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

func TestParseDurationAcceptsTheDocumentedForms(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want time.Duration
	}{
		{"30m", 30 * time.Minute},
		{"4h", 4 * time.Hour},
		{"2d", 48 * time.Hour},
		{"1d", 24 * time.Hour},
		{"90m", 90 * time.Minute},
		// Nothing given is the default, so a caller never has to special-case an
		// absent duration.
		{"", reservation.DefaultDuration},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got, err := reservation.ParseDuration(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestParseDurationRefusalListsTheAcceptedForms(t *testing.T) {
	for _, in := range []string{
		"4 hours", "two hours", "4H", "1w", "-2h", "0h", "1h30m", "4", "h", "10s",
		// Long enough to overflow the nanosecond counter. It must not wrap round
		// into a short duration and walk straight past the maximum.
		"1000000000000d", "99999999999999999999h",
	} {
		t.Run(in, func(t *testing.T) {
			got, err := reservation.ParseDuration(in)
			if err == nil {
				t.Fatalf("expected a refusal, got %s", got)
			}
			if !reservation.IsInvalidDuration(err) {
				t.Fatalf("expected an invalid-duration error, got %v", err)
			}
			// The whole point of the refusal is that the fix is in the message.
			for _, want := range []string{"30m", "4h", "2d"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal does not list %q: %s", want, err.Error())
				}
			}
		})
	}
}

// reservationsConfigMap returns a fixture override for the cluster's
// reservations ConfigMap. An empty maxDuration leaves the annotation out, which
// is the shape of a cluster that sets no limit of its own.
func reservationsConfigMap(maxDuration string) map[string]string {
	annotations := "    reservations.giantswarm.io/slack-channel: \"#reservations\"\n"
	if maxDuration != "" {
		annotations = "    reservations.giantswarm.io/max-duration: " + maxDuration + "\n" + annotations
	}

	return map[string]string{
		"management-clusters/" + fixtureCluster + "/" + reservation.ConfigMapFile: `apiVersion: v1
kind: ConfigMap
metadata:
  name: reservations
  namespace: giantswarm
  annotations:
` + annotations + `data: {}
`,
	}
}

func TestReserveRefusesADurationOverTheDefaultMaximum(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{extraFiles: reservationsConfigMap("")})
	req := testRequest(dir)
	req.Duration = 8 * 24 * time.Hour

	_, err := reservation.Reserve(req)
	if err == nil {
		t.Fatal("expected a refusal, got none")
	}
	if !reservation.IsInvalidDuration(err) {
		t.Fatalf("expected an invalid-duration error, got %v", err)
	}
	// The refusal has to name the limit, or the caller can only guess what to
	// ask for next.
	if !strings.Contains(err.Error(), "7d") {
		t.Errorf("refusal does not name the maximum: %s", err.Error())
	}

	// The maximum itself is still a reservation anybody may hold.
	atMax := testRequest(newGitOpsFixture(t, fixtureOptions{extraFiles: reservationsConfigMap("")}))
	atMax.Duration = 7 * 24 * time.Hour
	if _, err := reservation.Reserve(atMax); err != nil {
		t.Errorf("the maximum itself was refused: %v", err)
	}
}

func TestReserveTakesTheLowerOfTheClusterMaximumAndSevenDays(t *testing.T) {
	for _, tc := range []struct {
		name       string
		clusterMax string
		duration   time.Duration
		// wantNamed is what the refusal has to name. Empty means the
		// reservation is allowed.
		wantNamed string
	}{
		{"the cluster's lower limit binds", "12h", 24 * time.Hour, "12h"},
		{"and the cluster's own limit is allowed", "12h", 12 * time.Hour, ""},
		{"a cluster cannot raise the cap", "30d", 8 * 24 * time.Hour, "7d"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := newGitOpsFixture(t, fixtureOptions{extraFiles: reservationsConfigMap(tc.clusterMax)})
			req := testRequest(dir)
			req.Duration = tc.duration

			res, err := reservation.Reserve(req)
			if tc.wantNamed == "" {
				if err != nil {
					t.Fatal(err)
				}
				if got, want := res.Until, req.Now.Add(tc.duration); !got.Equal(want) {
					t.Errorf("until: got %s, want %s", got, want)
				}

				return
			}

			if err == nil {
				t.Fatal("expected a refusal, got none")
			}
			if !reservation.IsInvalidDuration(err) {
				t.Fatalf("expected an invalid-duration error, got %v", err)
			}
			if !strings.Contains(err.Error(), tc.wantNamed) {
				t.Errorf("refusal does not name the maximum %q: %s", tc.wantNamed, err.Error())
			}
		})
	}
}

func TestReserveRefusesAMalformedClusterMaximum(t *testing.T) {
	// The cluster's owners set the annotation to keep reservations short.
	// Falling back to the default on a typo would hand out the longest
	// reservation instead of the shortest.
	dir := newGitOpsFixture(t, fixtureOptions{extraFiles: reservationsConfigMap("forever")})

	_, err := reservation.Reserve(testRequest(dir))
	if err == nil {
		t.Fatal("expected a refusal, got none")
	}
	if !reservation.IsInvalidDuration(err) {
		t.Fatalf("expected an invalid-duration error, got %v", err)
	}
	for _, want := range []string{reservation.MaxDurationAnnotation, "forever", "30m", "4h", "2d"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not mention %q: %s", want, err.Error())
		}
	}
}

func TestReserveHonoursTheRequestedDuration(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})
	req := testRequest(dir)
	req.Duration = 4 * time.Hour

	res, err := reservation.Reserve(req)
	if err != nil {
		t.Fatal(err)
	}

	const (
		from  = "2026-09-15T10:00:00Z"
		until = "2026-09-15T14:00:00Z"
	)
	if got, want := res.Until, req.Now.Add(4*time.Hour); !got.Equal(want) {
		t.Errorf("until: got %s, want %s", got, want)
	}

	// The entry is what a release run and a human with kubectl read back, and the
	// annotations are what answers "why is this version running" on the cluster.
	// Both have to carry the same window.
	entry := reservationEntries(t, dir, fixtureCluster)[fixtureApp]
	if got := entry["from"]; got != from {
		t.Errorf("entry from: got %q, want %q", got, from)
	}
	if got := entry["until"]; got != until {
		t.Errorf("entry until: got %q, want %q", got, until)
	}

	source := mustObject(t, renderCluster(t, dir, fixtureCluster), "OCIRepository/"+res.SourceName)
	annotations, _ := source["metadata"].(map[string]any)["annotations"].(map[string]any)
	if got := annotations[reservation.AnnotationPrefix+"from"]; got != from {
		t.Errorf("annotation from: got %v, want %q", got, from)
	}
	if got := annotations[reservation.AnnotationPrefix+"until"]; got != until {
		t.Errorf("annotation until: got %v, want %q", got, until)
	}
}
