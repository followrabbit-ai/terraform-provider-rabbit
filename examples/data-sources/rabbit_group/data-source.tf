data "rabbit_group" "domain_admins" {
  name = "Domain admins"
}

output "domain_admin_principals" {
  value = data.rabbit_group.domain_admins.principals
}
