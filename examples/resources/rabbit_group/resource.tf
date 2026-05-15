resource "rabbit_group" "platform_admins" {
  name  = "Platform Admins"
  roles = ["roles/domain.editor"]

  scope = {
    folders  = ["123456789"] # bare GCP folder id, no "folders/" prefix
    projects = ["acme-prod"] # bare GCP project id, no "projects/" prefix
  }

  principals = [
    { name = "alice@acme.com", principal_type = "EMAIL" },
    { name = "platform-team@acme.com", principal_type = "EXTERNAL_GROUP" },
  ]
}
