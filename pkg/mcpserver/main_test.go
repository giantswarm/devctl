package mcpserver_test

import (
	"testing"

	"github.com/giantswarm/devctl/v8/internal/nocredentials"
)

// TestMain runs every case with each credential store failing on any
// access, and refuses a file of the server that imports one: the server acts
// only with the person's token it is handed.
func TestMain(m *testing.M) { nocredentials.Main(m) }
