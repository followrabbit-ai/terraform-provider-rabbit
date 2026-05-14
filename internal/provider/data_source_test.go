package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccDataRole_byName looks up the seeded "Domain Viewer" role.
func TestAccDataRole_byName(t *testing.T) {
	cfg := providerConfigHCL() + `
data "rabbit_role" "v" { name = "Domain Viewer" }
`
	resource.Test(t, resource.TestCase{
		PreCheck:                 preCheck(t),
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{{
			Config: cfg,
			Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttrSet("data.rabbit_role.v", "id"),
				resource.TestCheckResourceAttr("data.rabbit_role.v", "resource_type", "DOMAIN"),
			),
		}},
	})
}

// TestAccDataRole_byID looks up by id and confirms the name comes back.
func TestAccDataRole_byID(t *testing.T) {
	cfg := providerConfigHCL() + `
data "rabbit_role" "by_id" { id = "roles/domain.viewer" }
`
	resource.Test(t, resource.TestCase{
		PreCheck:                 preCheck(t),
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{{
			Config: cfg,
			Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("data.rabbit_role.by_id", "id", "roles/domain.viewer"),
				resource.TestCheckResourceAttrSet("data.rabbit_role.by_id", "name"),
			),
		}},
	})
}

// TestAccDataGroup_byID reads a freshly-created tfacc group via the data
// source. We can't safely point the data source at a pre-existing seeded group
// because the safety transport allows GETs on anything — but the test would
// then assume the seeded group's exact shape, which is brittle. So we round-
// trip our own.
func TestAccDataGroup_byID(t *testing.T) {
	name := uniqueName("ds-group")
	cfg := providerConfigHCL() + fmt.Sprintf(`
data "rabbit_role" "viewer" { name = "Domain Viewer" }
resource "rabbit_group" "g" {
  name  = %q
  roles = [data.rabbit_role.viewer.id]
  principals = [{ name = "alice@demo.io", principal_type = "EMAIL" }]
}
data "rabbit_group" "read" {
  id = rabbit_group.g.id
}
`, name)
	resource.Test(t, resource.TestCase{
		PreCheck:                 preCheck(t),
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{{
			Config: cfg,
			Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("data.rabbit_group.read", "name", name),
				resource.TestCheckResourceAttr("data.rabbit_group.read", "principals.#", "1"),
			),
		}},
	})
}
