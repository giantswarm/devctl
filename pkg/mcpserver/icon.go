package mcpserver

import (
	_ "embed"
	"encoding/base64"

	"github.com/mark3labs/mcp-go/mcp"
)

// logo is the Giant Swarm logo, a byte-identical copy of
// giantswarm/mcp-toolkit brand/giantswarm.svg (v0.5.2), which is minified
// from giantswarm/web-assets assets/app-icons/giantswarm/1/light.svg. It is
// copied rather than imported: mcp-toolkit would raise mcp-go and some
// thirty other modules.
//
//go:embed giantswarm.svg
var logo []byte

// Icon is the server's icon in its implementation info: the Giant Swarm logo
// as an SVG data URI of any size, as mcp-toolkit's brand.GiantSwarmIcon
// returns it.
func Icon() mcp.Icon {
	return mcp.Icon{
		Src:      "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(logo),
		MIMEType: "image/svg+xml",
		Sizes:    []string{"any"},
	}
}
