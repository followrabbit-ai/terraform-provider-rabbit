BINARY := terraform-provider-rabbit
VERSION ?= 0.0.1
OS_ARCH := $(shell go env GOOS)_$(shell go env GOARCH)
INSTALL_DIR := $(HOME)/.terraform.d/plugins/registry.terraform.io/followrabbit-ai/rabbit/$(VERSION)/$(OS_ARCH)

.PHONY: build install import-cli test testacc fmt vet tidy docs clean

build:
	go build -o $(BINARY) .

install: build
	mkdir -p $(INSTALL_DIR)
	mv $(BINARY) $(INSTALL_DIR)/$(BINARY)_v$(VERSION)

# Build the rabbit-tf-import companion CLI used to migrate an existing
# Rabbit setup into Terraform. See docs/MIGRATION.md.
import-cli:
	go build -o rabbit-tf-import ./cmd/rabbit-tf-import

tidy:
	go mod tidy

fmt:
	go fmt ./...
	terraform fmt -recursive examples/

vet:
	go vet ./...

test:
	go test -race -count=1 ./...

# Acceptance tests against a live Rabbit backend.
# Requires (all opaque to the suite — driven entirely by env):
#   RABBIT_ENDPOINT                       e.g. https://api.your-rabbit-host.example
#   RABBIT_AUDIENCE                       OAuth2 client ID the backend validates
#   RABBIT_TEST_DOMAIN_ID                 disposable test tenant the suite targets
#   RABBIT_TEST_ALLOWED_DOMAINS           CSV allow-list; must include the above
#   RABBIT_TEST_IMPERSONATE_SA_EMAIL      Rabbit impersonation SA (test-only auth)
#   RABBIT_TEST_IMPERSONATE_TARGET_EMAIL  User the impersonation token acts as
testacc:
	@if [ -z "$$RABBIT_TEST_DOMAIN_ID" ]; then echo "RABBIT_TEST_DOMAIN_ID not set"; exit 1; fi
	@if [ -z "$$RABBIT_TEST_ALLOWED_DOMAINS" ]; then echo "RABBIT_TEST_ALLOWED_DOMAINS not set"; exit 1; fi
	@if [ -z "$$RABBIT_TEST_IMPERSONATE_SA_EMAIL" ]; then echo "RABBIT_TEST_IMPERSONATE_SA_EMAIL not set"; exit 1; fi
	@if [ -z "$$RABBIT_TEST_IMPERSONATE_TARGET_EMAIL" ]; then echo "RABBIT_TEST_IMPERSONATE_TARGET_EMAIL not set"; exit 1; fi
	TF_ACC=1 go test -count=1 -timeout 30m -v ./internal/provider/...

docs:
	go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate

clean:
	rm -f $(BINARY) coverage.out
	rm -rf dist/
