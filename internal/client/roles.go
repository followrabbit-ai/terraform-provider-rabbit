package client

import (
	"context"
	"fmt"
	"net/url"
)

// ListRoles returns all roles for a resource type (BASE or DOMAIN).
func (c *Client) ListRoles(ctx context.Context, resourceType ResourceType) ([]Role, error) {
	var out []Role
	path := fmt.Sprintf("/api/role/v1/roles/%s", url.PathEscape(string(resourceType)))
	if err := c.Do(ctx, "GET", path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
