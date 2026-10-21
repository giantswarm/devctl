package rolloutwait

import (
	"testing"

	"github.com/giantswarm/devctl/v8/internal/nocredentials"
)

// TestMain runs every case with each credential store failing on any
// access: the engine acts only with the clients built from the tokens a
// test hands in.
func TestMain(m *testing.M) { nocredentials.Main(m) }
