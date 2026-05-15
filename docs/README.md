# Rabbit Terraform Provider

Manage [Rabbit](https://followrabbit.ai) access management as code. This
provider lets you declare Rabbit groups, role bindings, and resource scope in
Terraform alongside the rest of your cloud infrastructure.

- **Source:** `followrabbit-ai/rabbit` on the Terraform Registry
- **Status:** v1.x — stable schema; follows semver
- **License:** MPL-2.0

---

## Contents

- [Quick start](#quick-start)
- [Installation](#installation)
- [Authentication](#authentication)
- [Provider configuration](#provider-configuration)
- [Resources](#resources)
  - [`rabbit_group`](#rabbit_group)
  - [`rabbit_group_member`](#rabbit_group_member)
- [Data sources](#data-sources)
  - [`rabbit_group`](#rabbit_group-data-source)
- [Available roles](#available-roles)
- [Migrating an existing setup](#migrating-an-existing-setup)
- [Authoritative vs additive: when to use which resource](#authoritative-vs-additive)
- [Known limitations](#known-limitations)

---

## Quick start

```hcl
terraform {
  required_providers {
    rabbit = {
      source  = "followrabbit-ai/rabbit"
      version = "~> 1.0"
    }
  }
}

provider "rabbit" {
  domain_id = "acme.com"
}

resource "rabbit_group" "platform_admins" {
  name  = "Platform Admins"
  roles = ["roles/domain.editor"]

  principals = [
    { name = "alice@acme.com", principal_type = "EMAIL" },
    { name = "platform-team@acme.com", principal_type = "EXTERNAL_GROUP" },
  ]
}
```

```sh
gcloud auth application-default login   # one-time, per user
terraform init
terraform plan
terraform apply
```

---

## Installation

Terraform installs the provider automatically the first time you run
`terraform init` against a configuration that declares it. Pin to a minor
version so future breaking changes don't surprise you:

```hcl
terraform {
  required_version = ">= 1.5.0"   # 1.5+ for `import` blocks
  required_providers {
    rabbit = {
      source  = "followrabbit-ai/rabbit"
      version = "~> 1.0"
    }
  }
}
```

---

## Authentication

The provider authenticates to the Rabbit API by minting a Google ID token
whose audience matches Rabbit's OAuth2 client ID, then sending it as a
bearer token on every request. The credentials chain mirrors the official
Google provider, so most workflows work out of the box:

1. **`credentials`** — service account JSON, either inline or as a path,
   set via the provider attribute or `GOOGLE_CREDENTIALS` /
   `GOOGLE_APPLICATION_CREDENTIALS`.
2. **`impersonate_service_account`** — base credentials act as a token
   creator for a target service account; the ID token is minted by that SA
   via the IAM Credentials API.
3. **Application Default Credentials** — anything ADC finds, including
   `gcloud auth application-default login`, GCE/GKE metadata, Workload
   Identity Federation, etc. (See [end-user ADC limitation](#known-limitations).)

The Google account or service account used here must be a Rabbit user with
the relevant `domain.groups.*` permissions in the domain you're managing.

### Recommended setups

| Context | Recommended auth |
|---|---|
| **Interactive use** | `gcloud auth application-default login` with a service account impersonation (`impersonate_service_account`). |
| **CI / CD** | Workload Identity Federation → impersonate a dedicated Terraform SA. |
| **Local with SA key** | `credentials` pointing to a service account JSON. Use sparingly — long-lived keys are a liability. |

---

## Provider configuration

```hcl
provider "rabbit" {
  endpoint                    = "https://api.followrabbit.ai"  # optional
  audience                    = "..."                            # optional
  domain_id                   = "acme.com"
  credentials                 = file("path/to/sa.json")          # optional
  impersonate_service_account = "tf-rabbit@acme-prod.iam.gserviceaccount.com"  # optional
  request_timeout             = "30s"                            # optional
}
```

| Attribute | Type | Description |
|---|---|---|
| `endpoint` | string | Rabbit API base URL. Defaults to the prod endpoint. Env: `RABBIT_ENDPOINT`. |
| `audience` | string | OAuth2 client ID used as the ID token audience. Defaults to the prod client ID; override only for self-hosted or non-prod environments. Env: `RABBIT_AUDIENCE`. |
| `domain_id` | string | Default Rabbit domain (e.g. `"acme.com"`). Resources may override per-instance. Env: `RABBIT_DOMAIN_ID`. |
| `credentials` | string, sensitive | Inline service account JSON or path to a JSON file. Env: `GOOGLE_CREDENTIALS` / `GOOGLE_APPLICATION_CREDENTIALS`. |
| `impersonate_service_account` | string | Email of a service account to impersonate via Google IAM Credentials. Env: `GOOGLE_IMPERSONATE_SERVICE_ACCOUNT`. |
| `request_timeout` | string (duration) | Per-request HTTP timeout. Defaults to `30s`. |

---

## Resources

### `rabbit_group`

Manages a Rabbit group authoritatively — its name, role bindings, GCP
folder/project scope, and full principal list.

```hcl
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
    { name = "deploy-sa@acme-prod.iam.gserviceaccount.com", principal_type = "SERVICE_ACCOUNT" },
  ]
}
```

#### Arguments

| Name | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Display name for the group. Must be non-empty. |
| `roles` | set(string) | yes | Non-empty set of role IDs granted by this group, e.g. `"roles/domain.viewer"`. See [Available roles](#available-roles) for the full list. Each value is validated against the `roles/<namespace>.<name>` shape at plan time. |
| `principals` | set(object) | yes | Members of the group. See [Principal](#principal-object). |
| `scope` | object | no | Folder/project scope. Omit for domain-wide. See [Scope](#scope-object). |
| `domain_id` | string | no | Override the provider's `domain_id`. Forces replacement. |

#### Attributes

| Name | Type | Description |
|---|---|---|
| `id` | string | Server-assigned group identifier. |

#### Scope object

| Name | Type | Description |
|---|---|---|
| `folders` | set(string) | GCP folder IDs (e.g. `"folders/123456789"`). |
| `projects` | set(string) | GCP project IDs (e.g. `"projects/acme-prod"`). |

The folder/project IDs must correspond to resources Rabbit has crawled for
your domain; unknown IDs are rejected. Leave `scope` unset (or both lists
empty) for a domain-wide group.

#### Principal object

| Name | Type | Description |
|---|---|---|
| `name` | string | Email, service account email, group email, or domain. |
| `principal_type` | string | One of `EMAIL`, `TRANSITIVE_EMAIL`, `SERVICE_ACCOUNT`, `EXTERNAL_GROUP`, `DOMAIN`. |
| `id` | string (computed) | Server-assigned principal id, stable across updates. |

`TRANSITIVE_EMAIL` principals are derived by Rabbit from `EXTERNAL_GROUP`
memberships; you cannot create them directly.

#### Import

```sh
terraform import rabbit_group.platform_admins acme.com/abcd-1234
```

The import ID format is `<domain_id>/<group_id>`. If your provider has
`domain_id` set, you may omit the domain prefix.

### `rabbit_group_member`

Adds a single principal to a Rabbit group **additively** — without taking
authoritative control of the group's full principal list. Use this when the
group is managed elsewhere (UI, another Terraform module, an external
process) and you just want to ensure a specific principal exists in it.

> **Important:** do not combine `rabbit_group_member` with a
> `rabbit_group` declaration that lists the same group's principals in the
> same Terraform plan — the authoritative resource will fight the additive
> one. See [Authoritative vs additive](#authoritative-vs-additive).

Concurrent `terraform apply` operations against the same group are
serialised through a per-group mutex inside the provider, so adding several
members to one group in parallel is safe within a single process.

```hcl
resource "rabbit_group_member" "alice" {
  group_id       = "abcd-1234"
  name           = "alice@acme.com"
  principal_type = "EMAIL"
}
```

#### Arguments

| Name | Type | Required | Description |
|---|---|---|---|
| `group_id` | string | yes | ID of the group to add the principal to. Forces replacement. |
| `name` | string | yes | Principal name. Forces replacement. |
| `principal_type` | string | yes | One of `EMAIL`, `TRANSITIVE_EMAIL`, `SERVICE_ACCOUNT`, `EXTERNAL_GROUP`, `DOMAIN`. Forces replacement. |
| `domain_id` | string | no | Override the provider's `domain_id`. Forces replacement. |

#### Attributes

| Name | Type | Description |
|---|---|---|
| `id` | string | Composite id: `<domain>/<group_id>/<principal_type>/<name>`. |
| `principal_id` | string | Server-assigned principal id. |

#### Import

```sh
terraform import rabbit_group_member.alice acme.com/abcd-1234/EMAIL/alice@acme.com
```

---

## Data sources

### `rabbit_group` (data source)

Look up an existing Rabbit group by id or name.

```hcl
data "rabbit_group" "domain_admins" {
  name = "Domain admins"
}

output "principals" {
  value = data.rabbit_group.domain_admins.principals
}
```

#### Arguments

| Name | Type | Required | Description |
|---|---|---|---|
| `id` | string | one of | Group ID. |
| `name` | string | one of | Group display name. |
| `domain_id` | string | no | Override the provider's `domain_id`. |

#### Attributes

| Name | Type | Description |
|---|---|---|
| `id`, `name`, `domain_id` | string | Resolved values. |
| `roles` | set(string) | Role IDs granted by the group. |
| `scope` | object | `folders` and `projects` sets. |
| `principals` | set(object) | Members; same shape as `rabbit_group.principals`. |

---

## Available roles

The `roles` attribute on `rabbit_group` accepts these role IDs. They're
stable, customer-assignable identifiers — use them as string literals in
your Terraform config.

| Role ID | Name | What it grants |
|---|---|---|
| `roles/domain.viewer` | Domain Viewer | Read-only access to everything in the domain. |
| `roles/domain.editor` | Domain Editor | Read/write access to the domain's resources (groups, settings, cost data). |
| `roles/bigquery.editor` | BigQuery Editor | Edit access scoped to BigQuery within the domain. |
| `roles/gke.editor` | GKE Editor | Edit access scoped to GKE within the domain. |

`roles/domain.admin` exists but is reserved for Rabbit's built-in domain
admin group and **cannot be assigned** to user-created groups — the
backend rejects it. See [Known limitations](#known-limitations).

Additional product-internal roles (`roles/rabbit.*`) exist but aren't
customer-assignable.

---

## Migrating an existing setup

If your access setup already exists in Rabbit (managed through the UI or
another tool), you can adopt Terraform in three commands. This relies on
Terraform 1.5+ [`import` blocks][import-blocks] together with the
companion `rabbit-tf-import` CLI shipped in this repository.

[import-blocks]: https://developer.hashicorp.com/terraform/language/import

### TL;DR

```sh
rabbit-tf-import --domain acme.com > imports.tf
terraform plan -generate-config-out=generated.tf
terraform apply       # no-op; state now matches reality
```

### Step 1 — install the helper

```sh
go install github.com/followrabbit-ai/terraform-provider-rabbit/cmd/rabbit-tf-import@latest
```

The CLI reuses the provider's authentication, so the same flags and
environment variables work:

| Flag | Env |
|---|---|
| `--endpoint` | `RABBIT_ENDPOINT` |
| `--audience` | `RABBIT_AUDIENCE` |
| `--credentials` | `GOOGLE_CREDENTIALS` / `GOOGLE_APPLICATION_CREDENTIALS` |
| `--impersonate-service-account` | `GOOGLE_IMPERSONATE_SERVICE_ACCOUNT` |

### Step 2 — generate `import` blocks

```sh
rabbit-tf-import --domain acme.com > imports.tf
```

`imports.tf`:

```hcl
import {
  to = rabbit_group.platform_admins
  id = "acme.com/abcd-1234"
}

import {
  to = rabbit_group.viewers
  id = "acme.com/efgh-5678"
}
```

Resource names are derived from each group's display name; collisions get
`_2`, `_3` suffixes deterministically. Pass `--resource-prefix` when
importing several domains into one module to keep names disjoint.

By default only `rabbit_group` blocks are emitted. Pass `--include-members`
to also emit `rabbit_group_member` blocks per principal — only useful when
you deliberately want each principal managed by an additive resource (e.g.
one group split across modules).

### Step 3 — let Terraform write the HCL

Declare the provider, then:

```sh
terraform init
terraform plan -generate-config-out=generated.tf
```

Terraform calls the provider's `Read` for each `import` block and emits a
matching `resource "rabbit_group" "..." { ... }` stanza into
`generated.tf`. Review it, fold it into your module's file layout, then:

```sh
terraform apply
```

The apply should be empty — every resource was imported with its current
state, so there is nothing to change.

### Cleanup tips

- Delete `imports.tf` once the first apply succeeds. `import` blocks are
  single-use; leaving them in is harmless but noisy.

### Bringing in groups you create later

`rabbit-tf-import` is idempotent and produces deterministic output (sorted
by group name then id, principals by `(type, name)`). Re-run it any time
to spot UI-only changes or to import newly created groups.

---

## Authoritative vs additive

Two resources can both touch a group's principal list:

| Resource | Behaviour |
|---|---|
| `rabbit_group` | **Authoritative.** Owns the entire group, including its full principal list. Any drift is corrected on next apply. |
| `rabbit_group_member` | **Additive.** Owns one principal in a group it doesn't otherwise manage. Other principals are left alone. |

**Don't use both on the same group in the same Terraform plan.** Each
apply, the authoritative resource will remove principals added by the
additive resource and vice versa.

Choose:

- **`rabbit_group` alone** when one module / one team is the source of truth
  for the whole group. This is the common case.
- **`rabbit_group_member` alone** when the group is managed elsewhere (UI,
  legacy module, third party) and you want a single principal — typically a
  service account or a team email — to be present regardless.

---

## Known limitations

### Domain admin role and the built-in admin group

`roles/domain.admin` cannot be assigned to user-created groups; it is
reserved for Rabbit's built-in **Domain admins** group. Attempts to
assign it are rejected by the backend.

The Domain admins group itself can be imported with `rabbit_group` and
its **principal list managed through Terraform like any other group**
— add or remove members in the `principals` block and `terraform
apply`. The other fields are fixed: any attempt to rename it, change
its roles, or change its scope from Terraform will produce a clear
error from the backend listing the offending fields. In practice that
means declaring the resource with the existing values for those fields:

```hcl
resource "rabbit_group" "domain_admins" {
  name  = "Domain admins"                # immutable, must match reality
  roles = ["roles/domain.admin"]         # immutable, must match reality
  principals = [
    { name = "alice@acme.com", principal_type = "EMAIL" },
    { name = "bob@acme.com",   principal_type = "EMAIL" },
  ]
}
```

### Folder/project scope IDs must be known to Rabbit

`scope.folders` and `scope.projects` must reference resources Rabbit has
already discovered for your domain (via its GCP crawler). Unknown IDs are
rejected with a backend error. If you've just onboarded a project, give
the crawler a cycle to catch up before adding it to a scope.

### End-user ADC and Google ID tokens

Plain end-user ADC from `gcloud auth application-default login` produces
`authorized_user` credentials. Google's `idtoken` library, which the
provider uses to mint ID tokens for the Rabbit API, only supports service
account credentials. The practical workarounds:

- Use a service account JSON via `credentials`.
- Impersonate a service account via `impersonate_service_account`. This is
  the recommended pattern for CI and for interactive use with a
  workload-identity-friendly setup.

A future provider release may support end-user ADC directly by exchanging
the OAuth2 refresh token for an ID token through Google's token endpoint
(the same trick the Google provider uses for `id_token` audiences).

### Backend uses full-PUT semantics

The Rabbit backend has no atomic "add principal" endpoint — `rabbit_group_member`
implements add/remove via read-modify-write under a per-group mutex.
Concurrent applies *within a single Terraform process* are safe; concurrent
applies from *different processes* against the same group can race. Avoid
running multiple Terraform workspaces against the same group concurrently.

---

## Support

- Issues / feature requests: open an issue on the provider repository.
- Security reports: do not open public issues; email
  `security@followrabbit.ai`.
- Rabbit product documentation: <https://followrabbit.ai>.
