.PHONY: build test test-race test-relay test-extension lint vet cover repo-hygiene release-snapshot clean

BINARY=envsync

## Build the envsync binary
build:
	go build -o $(BINARY) .

## Run all tests
test:
	go test ./... -count=1 -timeout=120s

## Run tests with race detector
test-race:
	go test ./... -race -count=1 -timeout=120s

## Run relay tests
test-relay:
	cd relay && npm ci && npm test

## Compile extension
test-extension:
	cd extension && npm ci && npm run compile

## Run go vet
vet:
	go vet ./...

## Run golangci-lint (must be installed)
lint:
	golangci-lint run ./...

## Generate test coverage report
cover:
	go test ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## Check repo hygiene
repo-hygiene:
	bash ./scripts/check_repo_hygiene.sh

## GoReleaser dry-run (snapshot)
release-snapshot:
	goreleaser --snapshot --clean

## Clean build artifacts
clean:
	rm -f $(BINARY) $(BINARY).exe coverage.out coverage.html
	rm -rf dist/
	rm -rf .gocache/
	rm -rf extension/out relay/.wrangler/
	rm -f extension/*.vsix

## Show help
help:
	@echo "Available targets:"
	@echo "  build            Build the envsync binary"
	@echo "  test             Run all tests"
	@echo "  test-race        Run tests with race detector"
	@echo "  test-relay       Install and test the relay"
	@echo "  test-extension   Install and compile the VS Code extension"
	@echo "  vet              Run go vet"
	@echo "  lint             Run golangci-lint"
	@echo "  cover            Generate test coverage report"
	@echo "  repo-hygiene     Ensure no local/generated junk is present"
	@echo "  release-snapshot GoReleaser dry-run"
	@echo "  clean            Clean build artifacts"
