terraform {
  required_providers {
    rabbit = {
      source  = "followrabbit-ai/rabbit"
      version = "~> 1.0"
    }
  }
}

provider "rabbit" {
  # Defaults target the production Rabbit endpoint and OAuth2 client ID.
  # Override `endpoint` and `audience` (or RABBIT_ENDPOINT / RABBIT_AUDIENCE
  # env vars) for non-production environments.
  domain_id = "acme.com"
}
