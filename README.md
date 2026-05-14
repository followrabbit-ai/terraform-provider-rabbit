# terraform-provider-rabbit (developer docs)

User-facing documentation lives in [`docs/README.md`](docs/README.md) and is
what GitHub Pages publishes. **This file is for people working on the
provider itself.**

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
which Terraform picks up automatically. Combine with `dev_overrides` in
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

## Acceptance test harness

`internal/provider/acceptance_test.go` is unusual — it does much more than
a standard `terraform-plugin-testing` setup. The goal is that running the
suite against a live tenant can never disturb pre-existing access.
Guardrails, in order of how much you'd hate them being absent:

1. **Domain allow-list.** `RABBIT_TEST_DOMAIN_ID` must appear in the
   comma-separated `RABBIT_TEST_ALLOWED_DOMAINS`. Both env vars are
   required; the test process fails fast otherwise. Point this only at
   disposable test tenants.
2. **`tfacc-<runid>-` prefix.** `TestMain` generates a random prefix; every
   group / principal name created by the suite starts with it. The
   `safetyTransport` HTTP RoundTripper rejects any non-GET request whose
   JSON body has a `name` field that doesn't start with the prefix.
3. **Ownership tracking.** The same RoundTripper inspects `201 Created`
   responses and records the new group IDs. Any DELETE / PUT against
   `/api/v1/domains/{d}/groups/{id}` must target one of those IDs — so a
   buggy test that "imports" a pre-existing group cannot mutate it.
4. **Pre/post snapshot diff.** `TestMain` snapshots all groups in the
   domain before and after the run. Any change to a pre-existing group
   fails the suite even if all individual tests passed.
5. **Test-only auth path.** `client.NewRabbitImpersonationHTTPClient`
   produces an SA-signed JWT with a `target-email` claim — a mechanism
   the test backend accepts so the suite can act as any seeded user. It's
   installed into the provider via the `SetTestHTTPClientFactory` hook
   and is **never** exposed via the public provider schema.

### Configuration

Every Rabbit-specific value the suite needs comes from environment
variables. Configure these in your shell (for local runs) or as repository
variables/secrets in CI (`.github/workflows/testacc.yml` reads them):

| Env | Where it goes in CI | What it is |
|---|---|---|
| `RABBIT_ENDPOINT` | repo variable | API base URL of the test environment |
| `RABBIT_AUDIENCE` | repo variable | OAuth2 client ID the backend validates |
| `RABBIT_TEST_DOMAIN_ID` | repo variable | Disposable test tenant id |
| `RABBIT_TEST_ALLOWED_DOMAINS` | repo variable | CSV allow-list, must include the domain above |
| `RABBIT_TEST_IMPERSONATE_SA_EMAIL` | repo variable | SA that signs the test JWT |
| `RABBIT_TEST_IMPERSONATE_TARGET_EMAIL` | repo **secret** | User the test acts as; must be a Rabbit admin on the test tenant |

Local prerequisite: the developer's gcloud ADC needs
`roles/iam.serviceAccountTokenCreator` on `RABBIT_TEST_IMPERSONATE_SA_EMAIL`
so `iamcredentials.signJwt` works.

### Run

```sh
make testacc
```

Expected output on success:

```
[acc] domain=<TEST_TENANT> prefix=tfacc-XXXXXXXX- preSnapshot=N existing groups
...
PASS
ok  github.com/followrabbit-ai/terraform-provider-rabbit/internal/provider  ~80s
```

---

## CI workflows

- `.github/workflows/test.yml` — runs on every PR. `go vet`, `go test
  -race`, `terraform fmt -check examples/`. No secrets needed.
- `.github/workflows/testacc.yml` — `workflow_dispatch` + nightly schedule.
  Authenticates via Workload Identity Federation; all Rabbit-specific
  configuration is sourced from repo variables/secrets so the workflow
  file itself contains no tenant identifiers.
- `.github/workflows/release.yml` — runs on `v*` tags. GoReleaser builds
  multi-arch, signs with GPG (`GPG_PRIVATE_KEY` / `PASSPHRASE` repo
  secrets), publishes a GitHub Release containing the registry manifest.

---

## Release process

1. Make sure `master` is green and `docs/` reflects what's shipping.
2. Tag with a semver: `git tag vX.Y.Z && git push origin vX.Y.Z`.
3. GitHub Actions runs GoReleaser and publishes the release.
4. The Terraform Registry picks up the new release automatically once the
   provider is registered there (initial registration is a one-time
   manual step at <https://registry.terraform.io/publish/provider>).

---

## Coding conventions

- Plugin Framework, not SDKv2. New resources / data sources extend the
  factories in `internal/provider/provider.go`.
- Keep schema attribute descriptions short and customer-facing. The
  `terraform-plugin-docs` generator surfaces them verbatim.
- Conventional Commits (`feat:`, `fix:`, `docs:`, `chore:`, …).
- Acceptance tests **must** prefix every created resource name with
  `uniqueName(...)` so the safety transport allows them through.

---

## References

- Plugin Framework upstream — <https://developer.hashicorp.com/terraform/plugin/framework>
- Reference IAM-style patterns — `hashicorp/terraform-provider-google`,
  especially its `_iam_policy` / `_iam_binding` / `_iam_member` triad.
- Releasing providers — <https://developer.hashicorp.com/terraform/tutorials/providers-plugin-framework/providers-plugin-framework-release-publish>
