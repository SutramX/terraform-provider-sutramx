default: build

build:
	go build -o terraform-provider-sutramx .

install: build
	mkdir -p ~/.terraform.d/plugins/registry.terraform.io/sutramx/sutramx/0.1.0/$$(go env GOOS)_$$(go env GOARCH)
	cp terraform-provider-sutramx ~/.terraform.d/plugins/registry.terraform.io/sutramx/sutramx/0.1.0/$$(go env GOOS)_$$(go env GOARCH)/

fmt:
	gofmt -w .

lint:
	go vet ./...

# Unit tests, including plan/apply/import cycles against an in-memory fake API.
test:
	go test ./... -timeout 5m

# Acceptance tests against a real (disposable) workspace.
testacc:
	TF_ACC=1 go test ./internal/provider -run TestAcc -v -timeout 30m

docs:
	go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate

.PHONY: default build install fmt lint test testacc docs
