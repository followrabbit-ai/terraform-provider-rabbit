package provider

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/followrabbit-ai/terraform-provider-rabbit/internal/client"
)

// TestAccGroupMember_basic exercises the additive resource against a group
// created OUT-OF-BAND via the API (not by rabbit_group in the same plan).
// This is the supported usage: rabbit_group_member is for groups owned
// elsewhere (UI, another module, imported). Mixing it with an authoritative
// rabbit_group on the same target would race because the backend has
// full-PUT semantics.
//
// The test guarantees the group's pre-existing principal survives after the
// member is destroyed.
func TestAccGroupMember_basic(t *testing.T) {
	preCheck(t)() // gate before doing any out-of-band setup.
	groupID, domain, cleanup := setupOutOfBandGroup(t, "gm-basic", []client.Principal{
		{Name: "alice@demo.io", PrincipalType: client.PrincipalEmail},
	})
	t.Cleanup(cleanup)

	withMember := providerConfigHCL() + fmt.Sprintf(`
resource "rabbit_group_member" "extra" {
  domain_id      = %q
  group_id       = %q
  name           = "bob@demo.io"
  principal_type = "EMAIL"
}
`, domain, groupID)
	noMember := providerConfigHCL() // member resource removed → destroyed

	checkBackendPrincipals := func(want map[string]string) resource.TestCheckFunc {
		return func(s *terraform.State) error {
			c, err := newSnapshotClient(context.Background())
			if err != nil {
				return err
			}
			g, err := c.GetGroup(context.Background(), domain, groupID)
			if err != nil {
				return err
			}
			got := map[string]string{}
			for _, p := range g.Principals {
				got[p.Name] = string(p.PrincipalType)
			}
			if len(got) != len(want) {
				return fmt.Errorf("principal count: got %d (%v) want %d (%v)",
					len(got), got, len(want), want)
			}
			for k, v := range want {
				if got[k] != v {
					return fmt.Errorf("principal %q: got type %q want %q", k, got[k], v)
				}
			}
			return nil
		}
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 preCheck(t),
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: withMember,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("rabbit_group_member.extra", "name", "bob@demo.io"),
					checkBackendPrincipals(map[string]string{
						"alice@demo.io": "EMAIL",
						"bob@demo.io":   "EMAIL",
					}),
				),
			},
			{
				Config: noMember,
				Check:  checkBackendPrincipals(map[string]string{"alice@demo.io": "EMAIL"}),
			},
		},
	})
}

// setupOutOfBandGroup creates a tfacc-prefixed group via the raw API (bypassing
// Terraform) and returns its id, the domain, and a cleanup func. The id is
// registered with the safety transport so Terraform-driven member ops on it
// are allowed.
func setupOutOfBandGroup(t *testing.T, suffix string, principals []client.Principal) (id, domain string, cleanup func()) {
	t.Helper()
	domain = os.Getenv("RABBIT_TEST_DOMAIN_ID")
	c, err := newSnapshotClient(context.Background())
	if err != nil {
		t.Fatalf("setupOutOfBandGroup client: %v", err)
	}
	// Need at least one role; use the seeded Domain Viewer.
	roles, err := c.ListRoles(context.Background(), client.ResourceDomain)
	if err != nil {
		t.Fatalf("setupOutOfBandGroup roles: %v", err)
	}
	var viewer client.Role
	for _, r := range roles {
		if r.ID == "roles/domain.viewer" {
			viewer = r
			break
		}
	}
	if viewer.ID == "" {
		t.Fatalf("setupOutOfBandGroup: no roles/domain.viewer in dev")
	}
	g := &client.Group{
		Name:       uniqueName(suffix),
		Roles:      []client.Role{viewer},
		Principals: principals,
	}
	created, err := c.CreateGroup(context.Background(), domain, g)
	if err != nil {
		t.Fatalf("setupOutOfBandGroup create: %v", err)
	}
	rememberCreatedID(created.ID)
	return created.ID, domain, func() {
		_ = c.DeleteGroup(context.Background(), domain, created.ID)
	}
}

// TestAccGroupMember_duplicateIsNoop applying the same member twice should not
// create a duplicate principal.
func TestAccGroupMember_duplicateIsNoop(t *testing.T) {
	gName := uniqueName("gm-dup")
	cfg := providerConfigHCL() + fmt.Sprintf(`
resource "rabbit_group" "g" {
  name  = %q
  roles = ["roles/domain.viewer"]
  principals = [
    { name = "bob@demo.io", principal_type = "EMAIL" },
  ]
}
resource "rabbit_group_member" "dup" {
  group_id       = rabbit_group.g.id
  name           = "bob@demo.io"
  principal_type = "EMAIL"
}
`, gName)
	resource.Test(t, resource.TestCase{
		PreCheck:                 preCheck(t),
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{{
			Config: cfg,
			Check: resource.ComposeTestCheckFunc(
				func(s *terraform.State) error {
					rs := s.RootModule().Resources["rabbit_group.g"]
					c, _ := newSnapshotClient(context.Background())
					g, err := c.GetGroup(context.Background(),
						rs.Primary.Attributes["domain_id"], rs.Primary.ID)
					if err != nil {
						return err
					}
					count := 0
					for _, p := range g.Principals {
						if p.Name == "bob@demo.io" && p.PrincipalType == client.PrincipalEmail {
							count++
						}
					}
					if count != 1 {
						return fmt.Errorf("expected exactly 1 bob@demo.io, got %d", count)
					}
					return nil
				},
			),
		}},
	})
}

// TestAccGroupMember_import covers import via "<domain>/<group>/<type>/<name>".
func TestAccGroupMember_import(t *testing.T) {
	gName := uniqueName("gm-import")
	cfg := providerConfigHCL() + fmt.Sprintf(`
resource "rabbit_group" "g" {
  name  = %q
  roles = ["roles/domain.viewer"]
  principals = [{ name = "alice@demo.io", principal_type = "EMAIL" }]
}
resource "rabbit_group_member" "m" {
  group_id       = rabbit_group.g.id
  name           = "alice@demo.io"
  principal_type = "EMAIL"
}
`, gName)
	resource.Test(t, resource.TestCase{
		PreCheck:                 preCheck(t),
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:      "rabbit_group_member.m",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs := s.RootModule().Resources["rabbit_group_member.m"]
					return fmt.Sprintf("%s/%s/%s/%s",
						rs.Primary.Attributes["domain_id"],
						rs.Primary.Attributes["group_id"],
						rs.Primary.Attributes["principal_type"],
						rs.Primary.Attributes["name"]), nil
				},
				ImportStateVerifyIgnore: []string{"principal_id"},
			},
		},
	})
}
