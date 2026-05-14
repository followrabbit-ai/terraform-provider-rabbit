# terraform-provider-rabbit

Terraform provider for [Rabbit](https://followrabbit.ai), a cloud cost
management and optimization platform.

The v1 provider manages **Rabbit access management** — groups, role bindings,
and resource scope within a Rabbit domain.

## Authentication

The provider authenticates with your Google identity through Application
Default Credentials, the same way `gcloud` and the official Google provider
do. The most common setup is:

```sh
gcloud auth application-default login
```

The provider exchanges those credentials for a Google ID token scoped to the
Rabbit backend's audience and sends it as a bearer token on every API call.

Beyond plain ADC, the provider also accepts:

- `credentials` — path to a service account JSON file (`GOOGLE_CREDENTIALS` /
  `GOOGLE_APPLICATION_CREDENTIALS`).
- `impersonate_service_account` — impersonate a service account via the IAM
  Credentials API (`GOOGLE_IMPERSONATE_SERVICE_ACCOUNT`).

## Example

```hcl
terraform {
  required_providers {
    rabbit = {
      source  = "followrabbit-ai/rabbit"
      version = "~> 0.1"
    }
  }
}

provider "rabbit" {
  domain_id = "acme.com"
}

data "rabbit_role" "domain_admin" {
  name = "roles/domain.admin"
}

resource "rabbit_group" "platform_admins" {
  name  = "Platform Admins"
  roles = [data.rabbit_role.domain_admin.id]

  scope {
    projects = ["projects/acme-prod"]
  }

  principals {
    name           = "alice@acme.com"
    principal_type = "EMAIL"
  }
}

resource "rabbit_group_member" "bob" {
  group_id       = rabbit_group.platform_admins.id
  name           = "bob@acme.com"
  principal_type = "EMAIL"
}
```

## Development

```sh
make build          # compile
make test           # unit tests
make testacc        # acceptance tests against a live Rabbit (see Makefile for env)
make install        # install into ~/.terraform.d/plugins/ for local Terraform
make docs           # regenerate docs/ via terraform-plugin-docs
```
