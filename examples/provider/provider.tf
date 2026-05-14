terraform {
  required_providers {
    rabbit = {
      source  = "followrabbit-ai/rabbit"
      version = "~> 0.1"
    }
  }
}

provider "rabbit" {
  # Defaults to https://api.followrabbit.ai. Override for dev or self-hosted.
  # endpoint  = "https://dev.api.followrabbit.ai"
  # audience  = "532997547160-v5vbdm0abebklbv1kqt445l3unh15p9s.apps.googleusercontent.com"
  domain_id = "acme.com"
}
