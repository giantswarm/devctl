package list_test

import (
	"context"
	"io"
	"testing"

	"github.com/giantswarm/devctl/v8/cmd/reservation/gitops"
)

func TestListReleasesCheckoutOwnership(t *testing.T) {
	for _, missingCluster := range []bool{false, true} {
		dir := newCheckout(t, map[string]string{"management-clusters/graveler/configmap-reservations.yaml": "data: {}\n"})
		cluster := "graveler"
		if missingCluster {
			cluster = "absent"
		}
		cmd := newCommand(t, io.Discard)
		cmd.SetArgs([]string{repoDirFlag, dir, "--cluster", cluster})
		err := cmd.Execute()
		if (err != nil) != missingCluster {
			t.Fatalf("list: missing=%v: %v", missingCluster, err)
		}
		checkout, err := (&gitops.Flags{Dir: dir}).Open(context.Background(), io.Discard)
		if err != nil {
			t.Fatalf("list retained checkout ownership: %v", err)
		}
		if err := checkout.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
