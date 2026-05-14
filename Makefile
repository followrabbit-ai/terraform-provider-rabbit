BINARY := terraform-provider-rabbit
VERSION ?= 0.0.1
OS_ARCH := $(shell go env GOOS)_$(shell go env GOARCH)
INSTALL_DIR := $(HOME)/.terraform.d/plugins/registry.terraform.io/followrabbit-ai/rabbit/$(VERSION)/$(OS_ARCH)

.PHONY: build install test testacc fmt vet tidy docs clean

build:
	go build -o $(BINARY) .

install: build
	mkdir -p $(INSTALL_DIR)
	mv $(BINARY) $(INSTALL_DIR)/$(BINARY)_v$(VERSION)

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
# Requires:
#   RABBIT_ENDPOINT (e.g. https://dev.api.followrabbit.ai)
#   RABBIT_AUDIENCE (dev OAuth2 client ID)
#   RABBIT_TEST_DOMAIN_ID (must be demo.io or aliz.ai)
#   RABBIT_TEST_IMPERSONATE_SA_EMAIL
#   RABBIT_TEST_IMPERSONATE_TARGET_EMAIL
testacc:
	@if [ -z "$$RABBIT_TEST_DOMAIN_ID" ]; then echo "RABBIT_TEST_DOMAIN_ID not set"; exit 1; fi
	@if [ -z "$$RABBIT_TEST_IMPERSONATE_SA_EMAIL" ]; then echo "RABBIT_TEST_IMPERSONATE_SA_EMAIL not set"; exit 1; fi
	@if [ -z "$$RABBIT_TEST_IMPERSONATE_TARGET_EMAIL" ]; then echo "RABBIT_TEST_IMPERSONATE_TARGET_EMAIL not set"; exit 1; fi
	TF_ACC=1 go test -count=1 -timeout 30m -v ./internal/provider/...

docs:
	go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate

clean:
	rm -f $(BINARY) coverage.out
	rm -rf dist/
