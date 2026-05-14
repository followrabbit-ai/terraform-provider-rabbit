resource "rabbit_group" "platform_admins" {
  name  = "Platform Admins"
  roles = ["roles/domain.editor"]

  scope = {
    folders  = ["folders/123456789"]
    projects = ["projects/acme-prod"]
  }

  principals = [
    { name = "alice@acme.com", principal_type = "EMAIL" },
    { name = "platform-team@acme.com", principal_type = "EXTERNAL_GROUP" },
  ]
}
