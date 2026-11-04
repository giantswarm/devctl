package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/internal/versiongate"
)

func TestNancyCommandContract(t *testing.T) {
	var output bytes.Buffer
	root := newTestRoot(t, &output)
	command, _, err := root.Find([]string{"nancy"})
	if err != nil || command == root || command.Name() != "nancy" {
		t.Fatalf("nancy command not registered: %v", err)
	}
	if !versiongate.IsExempt(command.Annotations) {
		t.Fatal("local scanner would be blocked by the version gate")
	}
	if err := command.Args(command, []string{"unexpected"}); err == nil {
		t.Fatal("accepted positional arguments")
	}
	for _, name := range []string{"token", "username", "guide-token"} {
		if command.Flags().Lookup(name) != nil {
			t.Fatalf("exposed a credential flag: %s", name)
		}
	}
	root.SetArgs([]string{"nancy", "--help"})
	if err := root.Execute(); err != nil || !strings.Contains(output.String(), "devctl/ossindex") {
		t.Fatalf("help omitted credential setup: %v", err)
	}
	// Through Cobra's actual RunE, cancellation must precede keychain access
	// and any external command, even when a file-store override is present.
	t.Setenv("DEVCTL_KEYRING_FILE", t.TempDir()+"/must-not-be-read.json")
	root = newTestRoot(t, &output)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root.SetContext(ctx)
	root.SetArgs([]string{"nancy"})
	if err := root.Execute(); !errors.Is(err, context.Canceled) {
		t.Fatalf("command did not preserve cancellation: %v", err)
	}
}
