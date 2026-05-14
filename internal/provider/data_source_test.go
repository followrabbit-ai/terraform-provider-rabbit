package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccDataGroup_byID reads a freshly-created tfacc group via the data
// source. We can't safely point the data source at a pre-existing seeded group
// because the safety transport allows GETs on anything — but the test would
// then assume the seeded group's exact shape, which is brittle. So we round-
// trip our own.
func TestAccDataGroup_byID(t *testing.T) {
	name := uniqueName("ds-group")
	cfg := providerConfigHCL() + fmt.Sprintf(`
resource "rabbit_group" "g" {
  name  = %q
  roles = ["roles/domain.viewer"]
  principals = [{ name = "alice@example.com", principal_type = "EMAIL" }]
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
