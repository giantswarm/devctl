package reservation_test

import (
	"testing"

	"github.com/giantswarm/devctl/v8/internal/nocredentials"
)

// TestMain runs every case with each credential store failing on any access:
// the bundle chart pull is anonymous, and Reserve acts only on the repository
// it is handed.
func TestMain(m *testing.M) { nocredentials.Main(m) }
