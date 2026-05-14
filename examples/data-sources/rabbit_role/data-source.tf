data "rabbit_role" "viewer" {
  name = "Domain Viewer"
}

output "viewer_id" {
  value = data.rabbit_role.viewer.id
}
