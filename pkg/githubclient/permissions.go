package githubclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// acceptedPermission checks the devctl App's public record without the
// refused token. Failure to check leaves the original API refusal intact.
// The API root is the client's, including enterprise prefixes and test URLs.
func (c *Client) acceptedPermission(ctx context.Context, accepted string) string {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := c.ghClient.NewRequest(ctx, http.MethodGet, "apps/giantswarm-devctl", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.download.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ""
	}
	var app struct {
		Permissions map[string]string `json:"permissions"`
	}
	if err := json.Unmarshal(body, &app); err != nil {
		return ""
	}
	return heldPermissionSet(accepted, app.Permissions)
}

// GitHub separates alternative sets with semicolons and permissions that
// are all required with commas. Require a complete set at sufficient access.
func heldPermissionSet(accepted string, held map[string]string) string {
	for set := range strings.SplitSeq(accepted, ";") {
		members := strings.Split(set, ",")
		matches := true
		for _, member := range members {
			name, access, ok := strings.Cut(member, "=")
			name, access = strings.TrimSpace(name), strings.TrimSpace(access)
			if !ok || permissionLevel(access) == 0 || permissionLevel(held[name]) < permissionLevel(access) {
				matches = false
				break
			}
		}
		if matches {
			return strings.TrimSpace(set)
		}
	}
	return ""
}

func permissionLevel(access string) int {
	switch access {
	case "read":
		return 1
	case "write":
		return 2
	case "admin":
		return 3
	default:
		return 0
	}
}
