package mcpserver_test

import (
	"context"
	"encoding/base64"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/giantswarm/devctl/v8/pkg/mcpserver"
)

// TestInitializeAdvertisesTheGiantSwarmIcon: the initialize result carries
// the Giant Swarm logo, which muster stores as the MCPServer's status.icon.
func TestInitializeAdvertisesTheGiantSwarmIcon(t *testing.T) {
	svg, err := os.ReadFile("giantswarm.svg")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mcpserver.New(mcpserver.Config{}).Handler())
	t.Cleanup(srv.Close)
	c, err := client.NewStreamableHttpClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	res, err := c.Initialize(ctx, mcp.InitializeRequest{Params: mcp.InitializeParams{
		ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
		ClientInfo:      mcp.Implementation{Name: "test", Version: "0"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := []mcp.Icon{{
		Src:      "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(svg),
		MIMEType: "image/svg+xml",
		Sizes:    []string{"any"},
	}}
	if diff := cmp.Diff(want, res.ServerInfo.Icons); diff != "" {
		t.Errorf("serverInfo.icons (-want +got):\n%s", diff)
	}
}
