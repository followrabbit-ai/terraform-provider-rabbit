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

// TestAccGroup_basic creates a minimal group, lets Terraform refresh it
// (no-op plan), then destroys it. Verifies the happy path end-to-end.
func TestAccGroup_basic(t *testing.T) {
	name := uniqueName("basic")
	cfg := providerConfigHCL() + fmt.Sprintf(`
resource "rabbit_group" "g" {
  name  = %q
  roles = ["roles/domain.viewer"]
  scope = {
    folders  = []
    projects = []
  }
  principals = [
    { name = "alice@example.com", principal_type = "EMAIL" },
  ]
}
`, name)

	resource.Test(t, resource.TestCase{
		PreCheck:                 preCheck(t),
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("rabbit_group.g", "name", name),
					resource.TestCheckResourceAttrSet("rabbit_group.g", "id"),
					resource.TestCheckResourceAttr("rabbit_group.g", "principals.#", "1"),
					resource.TestCheckResourceAttr("rabbit_group.g", "roles.#", "1"),
				),
			},
			{
				// Re-apply with no changes — must be a no-op.
				Config:             cfg,
				PlanOnly:           true,
				ExpectNonEmptyPlan: false,
			},
		},
	})
}

// TestAccGroup_updateNameAndPrincipals updates the name (in-place) and
// swaps the principal set without recreating the group.
func TestAccGroup_updateNameAndPrincipals(t *testing.T) {
	nameA := uniqueName("upd-A")
	nameB := uniqueName("upd-B")
	tpl := func(n, princ string) string {
		return providerConfigHCL() + fmt.Sprintf(`
resource "rabbit_group" "g" {
  name  = %q
  roles = ["roles/domain.viewer"]
  principals = %s
}
`, n, princ)
	}
	configA := tpl(nameA, `[
    { name = "alice@example.com", principal_type = "EMAIL" },
  ]`)
	configB := tpl(nameB, `[
    { name = "bob@example.com",   principal_type = "EMAIL" },
    { name = "carol@example.com", principal_type = "EMAIL" },
  ]`)

	var firstID string
	captureID := func(s *terraform.State) error {
		rs := s.RootModule().Resources["rabbit_group.g"]
		if rs == nil {
			return fmt.Errorf("resource not in state")
		}
		firstID = rs.Primary.ID
		return nil
	}
	assertSameID := func(s *terraform.State) error {
		rs := s.RootModule().Resources["rabbit_group.g"]
		if rs == nil {
			return fmt.Errorf("resource not in state")
		}
		if rs.Primary.ID != firstID {
			return fmt.Errorf("id changed: %s -> %s (update should be in-place)", firstID, rs.Primary.ID)
		}
		return nil
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 preCheck(t),
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: configA,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("rabbit_group.g", "name", nameA),
					resource.TestCheckResourceAttr("rabbit_group.g", "principals.#", "1"),
					captureID,
				),
			},
			{
				Config: configB,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("rabbit_group.g", "name", nameB),
					resource.TestCheckResourceAttr("rabbit_group.g", "principals.#", "2"),
					assertSameID,
				),
			},
		},
	})
}

// TestAccGroup_principalTypes covers each PrincipalType the backend exposes.
func TestAccGroup_principalTypes(t *testing.T) {
	cases := []struct {
		typ  string
		name string
	}{
		{"EMAIL", "alice@example.com"},
		{"SERVICE_ACCOUNT", "demo-sa@demo.iam.gserviceaccount.com"},
		{"EXTERNAL_GROUP", "engineering@example.com"},
		{"DOMAIN", "example.com"},
		// TRANSITIVE_EMAIL is computed from external-group membership and
		// is not directly addable by the API. We skip it intentionally; the
		// schema validator still accepts it for import / drift cases.
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.typ, func(t *testing.T) {
			gName := uniqueName("ptype-" + tc.typ)
			cfg := providerConfigHCL() + fmt.Sprintf(`
resource "rabbit_group" "g" {
  name  = %q
  roles = ["roles/domain.viewer"]
  principals = [
    { name = %q, principal_type = %q },
  ]
}
`, gName, tc.name, tc.typ)
			resource.Test(t, resource.TestCase{
				PreCheck:                 preCheck(t),
				ProtoV6ProviderFactories: testProviderFactories,
				Steps: []resource.TestStep{{
					Config: cfg,
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr("rabbit_group.g", "principals.#", "1"),
					),
				}},
			})
		})
	}
}

// TestAccGroup_import tests `terraform import` against an existing group.
func TestAccGroup_import(t *testing.T) {
	name := uniqueName("import")
	cfg := providerConfigHCL() + fmt.Sprintf(`
resource "rabbit_group" "g" {
  name  = %q
  roles = ["roles/domain.viewer"]
  principals = [{ name = "alice@example.com", principal_type = "EMAIL" }]
}
`, name)
	resource.Test(t, resource.TestCase{
		PreCheck:                 preCheck(t),
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:                         "rabbit_group.g",
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateIdFunc:                    importIDForGroup("rabbit_group.g"),
				ImportStateVerifyIgnore:              []string{"principals"}, // id field on nested object is server-side
			},
		},
	})
}

func importIDForGroup(addr string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs := s.RootModule().Resources[addr]
		if rs == nil {
			return "", fmt.Errorf("resource %s not in state", addr)
		}
		return rs.Primary.Attributes["domain_id"] + "/" + rs.Primary.ID, nil
	}
}

// TestAccGroup_disappears removes the group via the API and expects the next
// plan to re-create it (drift handling).
func TestAccGroup_disappears(t *testing.T) {
	name := uniqueName("disappear")
	cfg := providerConfigHCL() + fmt.Sprintf(`
resource "rabbit_group" "g" {
  name  = %q
  roles = ["roles/domain.viewer"]
  principals = [{ name = "alice@example.com", principal_type = "EMAIL" }]
}
`, name)
	resource.Test(t, resource.TestCase{
		PreCheck:                 preCheck(t),
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeTestCheckFunc(
					func(s *terraform.State) error {
						// Delete via raw API.
						c, err := newSnapshotClient(context.Background())
						if err != nil {
							return err
						}
						// Bypass safety transport for cleanup of OUR own resource.
						rs := s.RootModule().Resources["rabbit_group.g"]
						return c.DeleteGroup(context.Background(),
							rs.Primary.Attributes["domain_id"], rs.Primary.ID)
					},
				),
				// Don't fail the test here; the next plan/refresh will detect the disappearance.
				ExpectNonEmptyPlan: true,
			},
			{
				// Now apply again — provider should re-create the group.
				Config: cfg,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("rabbit_group.g", "name", name),
				),
			},
		},
	})
}

// TestAccGroup_emptyScopeRoundTrip verifies the scope attribute behaves
// correctly when no folders/projects are assigned (the typical configuration
// for domain-wide groups).
//
// Non-empty scope can only be tested against real ResourceFolder /
// ResourceProject rows that exist in the test backend (Rabbit's backend
// enforces a FK relationship via ResourceService.checkIf*BelongToDomain,
// which returns 500 on unknown IDs). Those IDs depend on the dev crawler
// output and aren't stable enough to hardcode in the test suite. Customers
// should test non-empty scope locally against their own tenant.
func TestAccGroup_emptyScopeRoundTrip(t *testing.T) {
	name := uniqueName("scope-empty")
	cfg := providerConfigHCL() + fmt.Sprintf(`
resource "rabbit_group" "g" {
  name  = %q
  roles = ["roles/domain.viewer"]
  scope = {
    folders  = []
    projects = []
  }
  principals = [{ name = "alice@example.com", principal_type = "EMAIL" }]
}
`, name)
	resource.Test(t, resource.TestCase{
		PreCheck:                 preCheck(t),
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("rabbit_group.g", "scope.folders.#", "0"),
					resource.TestCheckResourceAttr("rabbit_group.g", "scope.projects.#", "0"),
				),
			},
			{Config: cfg, PlanOnly: true},
		},
	})
}

// silence unused-import warnings if a helper happens to be unused in a future trim.
var _ = client.PrincipalEmail
var _ = os.Getenv
