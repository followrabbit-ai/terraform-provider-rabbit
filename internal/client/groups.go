package client

import (
	"context"
	"fmt"
	"net/url"
)

// GroupsBasePath returns the Spring controller base path for a domain.
func GroupsBasePath(domainID string) string {
	return fmt.Sprintf("/api/v1/domains/%s/groups", url.PathEscape(domainID))
}

// GetGroup fetches a single group.
func (c *Client) GetGroup(ctx context.Context, domainID, groupID string) (*Group, error) {
	var out Group
	path := GroupsBasePath(domainID) + "/" + url.PathEscape(groupID)
	if err := c.Do(ctx, "GET", path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListGroups returns all groups for a domain, iterating through paginated
// Spring responses until exhaustion.
func (c *Client) ListGroups(ctx context.Context, domainID string) ([]Group, error) {
	var all []Group
	pageNum := 0
	pageSize := 200
	for {
		p, err := c.listGroupsPage(ctx, domainID, pageNum, pageSize)
		if err != nil {
			return nil, err
		}
		all = append(all, p.Content...)
		if p.Last || len(p.Content) == 0 || p.Number+1 >= p.TotalPages {
			return all, nil
		}
		pageNum = p.Number + 1
	}
}

func (c *Client) listGroupsPage(ctx context.Context, domainID string, pageNum, pageSize int) (*page[Group], error) {
	path := fmt.Sprintf("%s?page=%d&size=%d", GroupsBasePath(domainID), pageNum, pageSize)
	var out page[Group]
	if err := c.Do(ctx, "GET", path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateGroup creates a group.
func (c *Client) CreateGroup(ctx context.Context, domainID string, g *Group) (*Group, error) {
	var out Group
	if err := c.Do(ctx, "POST", GroupsBasePath(domainID), g, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateGroup replaces a group (PUT semantics — full body).
//
// One quirk: the backend's validateDomainAdminRole rejects any PUT whose
// body contains roles/domain.admin, regardless of which group is being
// updated. The Rabbit web UI works around this by stripping the role
// client-side before submitting (see frontend FlyoverContent.tsx). We do
// the same here so principal changes against the built-in Domain admins
// group succeed; the backend's isDomainAdminGroup branch ignores roles in
// the request for the admin group anyway, so dropping the role from the
// payload doesn't change the effective server-side state.
func (c *Client) UpdateGroup(ctx context.Context, domainID, groupID string, g *Group) (*Group, error) {
	body := *g
	if hasRole(body.Roles, "roles/domain.admin") {
		body.Roles = filterOutRole(body.Roles, "roles/domain.admin")
	}
	var out Group
	path := GroupsBasePath(domainID) + "/" + url.PathEscape(groupID)
	if err := c.Do(ctx, "PUT", path, &body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func hasRole(rs []Role, id string) bool {
	for _, r := range rs {
		if r.ID == id {
			return true
		}
	}
	return false
}

func filterOutRole(rs []Role, id string) []Role {
	out := make([]Role, 0, len(rs))
	for _, r := range rs {
		if r.ID != id {
			out = append(out, r)
		}
	}
	return out
}

// DeleteGroup removes a group.
func (c *Client) DeleteGroup(ctx context.Context, domainID, groupID string) error {
	path := GroupsBasePath(domainID) + "/" + url.PathEscape(groupID)
	return c.Do(ctx, "DELETE", path, nil, nil)
}
