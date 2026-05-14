# terraform-provider-rabbit (developer docs)

Internal developer-facing notes. **User-facing documentation lives in
[`docs/README.md`](docs/README.md)** and is what GitHub Pages publishes.

This document is for people **working on** the provider — building it,
testing it, releasing it. Don't surface internal hostnames or service
accounts in `docs/README.md`; they belong here.

---

## Layout

```
.
├── main.go                          # providerserver.Serve entry point
├── internal/
│   ├── provider/                    # Plugin Framework provider + resources
│   │   ├── provider.go
│   │   ├── resource_group.go
│   │   ├── resource_group_member.go
│   │   ├── data_source_role.go
│   │   ├── data_source_group.go
│   │   ├── conv.go                  # state ↔ client.Group conversions
│   │   ├── acceptance_test.go       # TF_ACC harness + safety guardrails
│   │   └── *_test.go
│   ├── client/                      # Rabbit REST API client
│   │   ├── client.go, auth.go
│   │   ├── groups.go, roles.go
│   │   ├── types.go, errors.go
│   │   └── *_test.go
│   └── mutex/                       # mutexkv-style keyed mutex for group_member
├── cmd/
│   └── rabbit-tf-import/            # migration helper CLI
├── examples/                        # used by terraform-plugin-docs
├── docs/                            # GitHub Pages source (user-facing)
└── .github/workflows/               # test / testacc / release
```

`internal/` keeps everything implementation-internal. The only Go API the
project exposes outside the provider binary is `cmd/rabbit-tf-import`.

---

## Day-to-day commands

```sh
make build          # compile the provider
make import-cli     # compile the rabbit-tf-import migration helper
make test           # unit tests (no network)
make testacc        # acceptance tests against a live Rabbit (see below)
make install        # install the provider into ~/.terraform.d/plugins/
make fmt            # go fmt + terraform fmt -recursive examples/
make vet            # go vet ./...
make docs           # regenerate per-resource docs via terraform-plugin-docs
```

`make install` writes to
`~/.terraform.d/plugins/registry.terraform.io/followrabbit-ai/rabbit/<version>/<os_arch>/`,
which Terraform picks up automatically; combine with `dev_overrides` in
`~/.terraformrc` to point at an in-progress build:

```hcl
provider_installation {
  dev_overrides {
    "followrabbit-ai/rabbit" = "/Users/<you>/.terraform.d/plugins/registry.terraform.io/followrabbit-ai/rabbit/0.0.1/darwin_arm64"
  }
  direct {}
}
```

---

## Authentication during development

Production auth uses Google ADC + ID token (see `docs/README.md` for the
user-facing description). Internally we also have a **Rabbit-specific
impersonation** path used by the acceptance suite and the migration CLI:
the dev backend's `BaseWebSecurityConfig` accepts an SA-signed JWT with an
`email` claim equal to the impersonation SA and a `target-email` claim
naming the user to act as. The `impersonate-user` skill in `rabbit-app`
documents the same trick.

| Env | Dev value |
|---|---|
| `RABBIT_ENDPOINT` | `https://dev.api.followrabbit.ai` |
| `RABBIT_AUDIENCE` | `532997547160-v5vbdm0abebklbv1kqt445l3unh15p9s.apps.googleusercontent.com` |
| `RABBIT_TEST_DOMAIN_ID` | `demo.io` (or `aliz.ai`) |
| `RABBIT_TEST_IMPERSONATE_SA_EMAIL` | `impersonation@rbt-dev-app-eu.iam.gserviceaccount.com` |
| `RABBIT_TEST_IMPERSONATE_TARGET_EMAIL` | a member of `rabbit_admins`, e.g. `csaba.kassai@followrabbit.ai` |

Caller's gcloud ADC must hold `roles/iam.serviceAccountTokenCreator` on the
impersonation SA. The same env vars also let `rabbit-tf-import` reach dev.

For prod, use a normal service account in `--credentials` /
`--impersonate-service-account` with the relevant `domain.groups.*`
permissions in the target domain.

---

## Acceptance test harness

`internal/provider/acceptance_test.go` is unusual — it does much more than
a standard `terraform-plugin-testing` setup. The goal is that running the
suite against a live tenant can never disturb pre-existing access. The
guardrails, in order of how much you'd hate them being absent:

1. **Domain allow-list.** `RABBIT_TEST_DOMAIN_ID` must be `demo.io` or
   `aliz.ai`. Anything else fails fast. Don't widen this list without a
   very good reason.
2. **`tfacc-<runid>-` prefix.** `TestMain` generates a random prefix; every
   group / principal name created by the suite starts with it. The
   `safetyTransport` HTTP RoundTripper rejects any non-GET request whose
   JSON body has a `name` field not starting with the prefix.
3. **Ownership tracking.** The same RoundTripper inspects `201 Created`
   responses and records the new group IDs. Any DELETE / PUT against
   `/api/v1/domains/{d}/groups/{id}` must target one of those IDs — so a
   buggy test that "imports" a pre-existing group cannot mutate it.
4. **Pre/post snapshot diff.** `TestMain` snapshots all groups in the
   domain before and after the run. Any change to a pre-existing group
   fails the suite even if all individual tests passed.
5. **Test-only auth path.** The Rabbit-impersonation transport is in
   `client.NewRabbitImpersonationHTTPClient`, installed into the provider
   via the `SetTestHTTPClientFactory` hook. The public provider schema
   never exposes it.

Run:

```sh
RABBIT_ENDPOINT=https://dev.api.followrabbit.ai \
RABBIT_AUDIENCE=532997547160-v5vbdm0abebklbv1kqt445l3unh15p9s.apps.googleusercontent.com \
RABBIT_TEST_DOMAIN_ID=demo.io \
RABBIT_TEST_IMPERSONATE_SA_EMAIL=impersonation@rbt-dev-app-eu.iam.gserviceaccount.com \
RABBIT_TEST_IMPERSONATE_TARGET_EMAIL=you@followrabbit.ai \
make testacc
```

Expected output on success:

```
[acc] domain=demo.io prefix=tfacc-XXXXXXXX- preSnapshot=2 existing groups
...
PASS
ok  github.com/followrabbit-ai/terraform-provider-rabbit/internal/provider  ~80s
```

---

## CI workflows

- `.github/workflows/test.yml` — runs on every PR. `go vet`, `go test -race`,
  `terraform fmt -check examples/`. No secrets needed.
- `.github/workflows/testacc.yml` — `workflow_dispatch` + nightly schedule.
  Authenticates via Workload Identity Federation (`WIF_PROVIDER` and
  `WIF_SERVICE_ACCOUNT` org secrets), targets `demo.io` in dev. Needs
  `RABBIT_TEST_IMPERSONATE_TARGET_EMAIL` org secret pointed at a rotating
  rabbit_admins member.
- `.github/workflows/release.yml` — runs on `v*` tags. GoReleaser builds
  multi-arch, signs with GPG (`GPG_PRIVATE_KEY` / `GPG_PASSPHRASE` org
  secrets), publishes a GitHub release containing the registry manifest.

---

## Release process

1. Make sure `master` is green and `docs/` reflects what's shipping.
2. Tag with a semver: `git tag v0.x.y && git push origin v0.x.y`.
3. GitHub Actions runs GoReleaser and publishes the release.
4. The Terraform Registry picks the new release up automatically once the
   provider is registered there (initial registration is a one-time
   manual step at <https://registry.terraform.io/publish/provider>).

GPG key fingerprint, registry credentials, etc. live in the team's
shared password store — never commit them.

---

## Coding conventions

- Plugin Framework, not SDKv2. New resources/data sources extend the
  factories in `internal/provider/provider.go`.
- Keep schema attribute descriptions short and customer-facing. The
  `terraform-plugin-docs` generator surfaces them verbatim.
- Conventional Commits (`feat:`, `fix:`, `docs:`, etc.) — see
  `rabbit-org-wide:commit-commands:commit` for the shared rules.
- Tests follow the standard Go layout. Acceptance tests **must** prefix
  every created name with `uniqueName(...)` so the safety transport allows
  them through.

---

## Pointers

- Backend API contract (Spring) — `rabbit-app/backend/src/main/java/...`,
  specifically `GroupController.java`, `RoleController.java`,
  `BaseWebSecurityConfig.java`, `GroupService.validateResourceScopes`,
  `GroupService.validateDomainAdminRole`.
- DTOs — `rabbit-app/backend/src/main/java/ai/aliz/rabbit/dto/accessmanagement/`.
- Seed access-management data in dev —
  `rabbit-app/backend/src/main/resources/migration/sql/access-management-test-env.sql`.
- Plugin Framework upstream — <https://developer.hashicorp.com/terraform/plugin/framework>.
- Reference patterns — `hashicorp/terraform-provider-google`, especially
  its IAM policy/binding/member triad helpers.
