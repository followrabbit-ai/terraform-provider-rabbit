resource "rabbit_group_member" "bob" {
  group_id       = rabbit_group.platform_admins.id
  name           = "bob@acme.com"
  principal_type = "EMAIL"
}
