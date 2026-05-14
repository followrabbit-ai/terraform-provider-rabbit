package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestClient(t *testing.T, handler http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := New(Config{Endpoint: srv.URL, HTTP: srv.Client()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, srv
}

func TestCreateGroup_requestShape(t *testing.T) {
	var captured Group
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method=%s want POST", r.Method)
		}
		if got, want := r.URL.Path, "/api/v1/domains/example.com/groups"; got != want {
			t.Errorf("path=%q want %q", got, want)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("body unmarshal: %v body=%s", err, body)
		}
		captured.ID = "g1"
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(captured)
	}))

	in := &Group{
		Name:  "Platform Admins",
		Roles: []Role{{ID: "roles/domain.admin"}},
		Scope: AccessScope{
			Folders:  []ResourceFolder{{ID: "folders/123"}},
			Projects: []ResourceProject{{ID: "projects/acme"}},
		},
		Principals: []Principal{{Name: "alice@x", PrincipalType: PrincipalEmail}},
	}
	got, err := c.CreateGroup(context.Background(), "example.com", in)
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if got.ID != "g1" {
		t.Errorf("returned id=%q want g1", got.ID)
	}
	if captured.Name != in.Name || len(captured.Roles) != 1 || captured.Roles[0].ID != "roles/domain.admin" {
		t.Errorf("payload mismatch: %+v", captured)
	}
}

func TestGetGroup_404(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	_, err := c.GetGroup(context.Background(), "example.com", "missing")
	if !IsNotFound(err) {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestUpdateGroup_putsFullBody(t *testing.T) {
	var seen Group
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method=%s want PUT", r.Method)
		}
		if got, want := r.URL.Path, "/api/v1/domains/example.com/groups/g1"; got != want {
			t.Errorf("path=%q want %q", got, want)
		}
		_ = json.NewDecoder(r.Body).Decode(&seen)
		seen.ID = "g1"
		_ = json.NewEncoder(w).Encode(seen)
	}))

	_, err := c.UpdateGroup(context.Background(), "example.com", "g1", &Group{
		Name:       "renamed",
		Roles:      []Role{{ID: "roles/domain.viewer"}},
		Principals: []Principal{{Name: "bob@x", PrincipalType: PrincipalEmail}},
	})
	if err != nil {
		t.Fatalf("UpdateGroup: %v", err)
	}
	if seen.Name != "renamed" || len(seen.Principals) != 1 {
		t.Errorf("PUT body wrong: %+v", seen)
	}
}

func TestDeleteGroup(t *testing.T) {
	called := false
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != http.MethodDelete {
			t.Errorf("method=%s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	if err := c.DeleteGroup(context.Background(), "example.com", "g1"); err != nil {
		t.Fatalf("DeleteGroup: %v", err)
	}
	if !called {
		t.Error("handler not called")
	}
}

func TestListGroups_paginates(t *testing.T) {
	pages := []page[Group]{
		{Content: []Group{{ID: "g1"}, {ID: "g2"}}, Number: 0, TotalPages: 2, Last: false},
		{Content: []Group{{ID: "g3"}}, Number: 1, TotalPages: 2, Last: true},
	}
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pageNum := r.URL.Query().Get("page")
		idx := 0
		if pageNum == "1" {
			idx = 1
		}
		_ = json.NewEncoder(w).Encode(pages[idx])
	}))
	got, err := c.ListGroups(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("got %d groups, want 3", len(got))
	}
}

func TestListRoles(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/api/role/v1/roles/DOMAIN"; got != want {
			t.Errorf("path=%q want %q", got, want)
		}
		_ = json.NewEncoder(w).Encode([]Role{{ID: "roles/domain.admin", Name: "Domain Admin", ResourceType: ResourceDomain}})
	}))
	roles, err := c.ListRoles(context.Background(), ResourceDomain)
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if len(roles) != 1 || roles[0].ID != "roles/domain.admin" {
		t.Errorf("unexpected: %+v", roles)
	}
}
