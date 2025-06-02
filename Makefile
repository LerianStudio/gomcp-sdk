.PHONY: all build test lint clean install-tools coverage benchmark integration-test security-scan

# Variables
GOPATH := $(shell go env GOPATH)
GOBIN := $(GOPATH)/bin
GOLANGCI_LINT_VERSION := v1.62.2

all: lint test build

build:
	@echo "Building..."
	@go build -v ./...
	@echo "Building examples..."
	@cd examples && for dir in */; do \
		if [ -f "$$dir/go.mod" ]; then \
			echo "Building example: $$dir"; \
			cd "$$dir" && go build -v ./... && cd ..; \
		fi \
	done
	@echo "Building tools..."
	@cd tools && for dir in */; do \
		if [ -f "$$dir/main.go" ]; then \
			echo "Building tool: $$dir"; \
			cd "$$dir" && go build -v . && cd ..; \
		fi \
	done

test:
	@echo "Running tests..."
	@go test -v -race -coverprofile=coverage.txt -covermode=atomic ./...

lint:
	@echo "Running linters..."
	@if ! which golangci-lint > /dev/null; then \
		echo "Installing golangci-lint..."; \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION); \
	fi
	@golangci-lint run --timeout=5m

clean:
	@echo "Cleaning..."
	@go clean -cache -testcache -modcache
	@rm -f coverage.txt coverage.html
	@find . -name "*.test" -type f -delete
	@find . -name "*.out" -type f -delete

install-tools:
	@echo "Installing development tools..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	@go install github.com/securego/gosec/v2/cmd/gosec@latest
	@go install github.com/google/go-licenses@latest
	@go install golang.org/x/tools/cmd/goimports@latest
	@go install golang.org/x/tools/cmd/godoc@latest
	@go install github.com/axw/gocov/gocov@latest
	@go install github.com/AlekSi/gocov-xml@latest

coverage:
	@echo "Running test coverage..."
	@go test -v -race -coverprofile=coverage.txt -covermode=atomic ./...
	@go tool cover -html=coverage.txt -o coverage.html
	@echo "Coverage report generated: coverage.html"

coverage-report: coverage
	@go tool cover -func=coverage.txt

benchmark:
	@echo "Running benchmarks..."
	@go test -bench=. -benchmem -timeout=20m ./...

integration-test:
	@echo "Running integration tests..."
	@go test -v -tags=integration -timeout=10m ./...

security-scan:
	@echo "Running security scan..."
	@if ! which gosec > /dev/null; then \
		echo "Installing gosec..."; \
		go install github.com/securego/gosec/v2/cmd/gosec@latest; \
	fi
	@gosec -fmt=text -severity=medium ./...

# Docker targets
docker-build:
	@echo "Building Docker image..."
	@docker build -t gomcp-sdk:latest .

docker-compose-up:
	@echo "Starting services with docker-compose..."
	@docker-compose up -d

docker-compose-down:
	@echo "Stopping services..."
	@docker-compose down

# Development helpers
fmt:
	@echo "Formatting code..."
	@go fmt ./...
	@goimports -w -local github.com/LerianStudio/gomcp-sdk .

vet:
	@echo "Running go vet..."
	@go vet ./...

mod-tidy:
	@echo "Tidying go modules..."
	@go mod tidy

mod-verify:
	@echo "Verifying go modules..."
	@go mod verify

# CI simulation
ci: clean lint test integration-test benchmark security-scan build
	@echo "CI pipeline completed successfully!"

# Help
help:
	@echo "Available targets:"
	@echo "  all              - Run lint, test, and build"
	@echo "  build            - Build the project and examples"
	@echo "  test             - Run unit tests with coverage"
	@echo "  lint             - Run golangci-lint"
	@echo "  clean            - Clean build artifacts and cache"
	@echo "  install-tools    - Install development tools"
	@echo "  coverage         - Generate coverage report"
	@echo "  coverage-report  - Display coverage summary"
	@echo "  benchmark        - Run benchmarks"
	@echo "  integration-test - Run integration tests"
	@echo "  security-scan    - Run security scanning"
	@echo "  docker-build     - Build Docker image"
	@echo "  fmt              - Format code"
	@echo "  vet              - Run go vet"
	@echo "  mod-tidy         - Tidy go modules"
	@echo "  ci               - Run full CI pipeline locally"
	@echo "  help             - Show this help message"