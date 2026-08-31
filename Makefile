# Build variables
BINARY_NAME=logwatch-analyzer
BUILD_DIR=bin
INSTALL_DIR=/opt/logwatch-ai
GO=go

GOLANGCI_LINT_VERSION := v2.13.1
GOFUMPT_VERSION       := v0.11.0

.DEFAULT_GOAL := help

.PHONY: all help build build-prod build-linux-amd64 build-darwin-arm64 build-all-platforms \
        test test-race coverage coverage-html fmt fmt-check vet lint lint-go check deps tidy clean install-tools \
        install run \
        preflight deploy deploy-stage rollback status lint-sh lint-payloads test-sh

# Version info from git
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

# Linker flags for version injection
LDFLAGS_VERSION=-X main.version=$(VERSION) -X main.gitCommit=$(GIT_COMMIT) -X main.buildTime=$(BUILD_TIME)

all: build ## Build the default local/dev binary

build: ## Build fast local/dev binary for host platform
	@echo "Building $(BINARY_NAME) $(VERSION)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build -ldflags="$(LDFLAGS_VERSION)" -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/analyzer

build-prod: ## Build optimized host production binary
	@echo "Building $(BINARY_NAME) $(VERSION) for production..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build -trimpath -ldflags="-s -w $(LDFLAGS_VERSION)" -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/analyzer

build-linux-amd64: ## Build optimized static Linux AMD64 production binary
	@echo "Building $(BINARY_NAME) $(VERSION) for Linux AMD64..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v3 \
		$(GO) build -trimpath -ldflags="-s -w $(LDFLAGS_VERSION)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/analyzer

build-darwin-arm64: ## Build optimized Darwin ARM64 production binary
	@echo "Building $(BINARY_NAME) $(VERSION) for macOS ARM64..."
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=arm64 \
		$(GO) build -trimpath -ldflags="-s -w $(LDFLAGS_VERSION)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/analyzer

build-all-platforms: build-linux-amd64 build-darwin-arm64 ## Build all production platform binaries
	@echo "All platform builds complete!"
	@ls -lh $(BUILD_DIR)/$(BINARY_NAME)-*

test: ## Run all tests
	@echo "Running tests..."
	$(GO) test ./...

test-race: ## Run tests with race detector
	$(GO) test -race ./...

coverage: ## Run tests with coverage summary
	$(GO) test -cover ./...

coverage-html: ## Generate HTML coverage report
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

fmt: ## Format code with gofumpt
	@echo "Formatting code..."
	gofumpt -w .

fmt-check: ## Fail if gofumpt would reformat files
	@out=$$(gofumpt -l .); \
	if [ -n "$$out" ]; then \
		echo "gofumpt would reformat:"; \
		echo "$$out"; \
		exit 1; \
	fi

vet: ## Run go vet
	@echo "Running go vet..."
	$(GO) vet ./...

lint-go: ## Run golangci-lint
	golangci-lint run ./...

lint-sh: ## Lint shell scripts with shellcheck (skipped if not installed)
	@if command -v shellcheck >/dev/null 2>&1; then \
		shellcheck -x deploy/*.sh; \
	else \
		echo "WARN: shellcheck not installed — skipping shell lint."; \
		echo "      Install it (brew install shellcheck / apt-get install shellcheck)"; \
		echo "      or see 'make install-tools'. CI enforces this regardless."; \
	fi

test-sh: ## Run shell unit tests for the deploy helpers
	bash deploy/lib_test.sh

lint-payloads: ## Lint the remote SSH payloads (shellcheck cannot see inside heredocs)
	@if command -v shellcheck >/dev/null 2>&1; then \
		bash deploy/lint-payloads.sh; \
	else \
		echo "WARN: shellcheck not installed — skipping payload lint."; \
	fi

lint: lint-go lint-sh lint-payloads ## Run all linters

check: fmt-check vet lint test test-sh ## Run the full local quality gate

deps: ## Download Go module dependencies
	$(GO) mod download

tidy: ## Tidy Go modules
	$(GO) mod tidy

clean: ## Remove build artifacts
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@rm -f coverage.out coverage.html

install-tools: ## Install pinned developer tools
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	$(GO) install mvdan.cc/gofumpt@$(GOFUMPT_VERSION)
	@# shellcheck is not a Go tool, so it cannot be `go install`ed. lint-sh
	@# degrades to a warning without it; CI always enforces it.
	@if command -v shellcheck >/dev/null 2>&1; then \
		echo "shellcheck: $$(shellcheck --version | awk '/version:/{print $$2}') (already installed)"; \
	elif command -v brew >/dev/null 2>&1; then \
		brew install shellcheck; \
	elif command -v apt-get >/dev/null 2>&1; then \
		echo "run: sudo apt-get install -y shellcheck"; \
	else \
		echo "install shellcheck manually: https://www.shellcheck.net/"; \
	fi

install: build-prod ## Install optimized binary to system directory
	@echo "Installing to $(INSTALL_DIR)..."
	@sudo mkdir -p $(INSTALL_DIR)
	@sudo cp $(BUILD_DIR)/$(BINARY_NAME) $(INSTALL_DIR)/
	@sudo cp -r scripts $(INSTALL_DIR)/
	@sudo chmod +x $(INSTALL_DIR)/scripts/*.sh
	@sudo mkdir -p $(INSTALL_DIR)/data
	@sudo mkdir -p $(INSTALL_DIR)/logs
	@if [ ! -f $(INSTALL_DIR)/.env ]; then \
		sudo cp configs/.env.example $(INSTALL_DIR)/.env; \
		echo "Created .env file - please configure it"; \
	fi
	@echo "Installation complete!"

run: build ## Build and run the application
	@$(BUILD_DIR)/$(BINARY_NAME)

# --- Remote deployment -------------------------------------------------
# Target host comes from deploy/deploy.env (gitignored); override with
# HOST=... on the command line. Always run `make preflight` first.

preflight: ## Inspect the production host read-only and check deploy gates
	@./deploy/preflight.sh $(HOST)

deploy-stage: ## Build and upload to the host's /tmp without touching the install
	@./deploy/deploy.sh --stage-only $(HOST)

deploy: ## Build, verify and install to the production host
	@./deploy/deploy.sh $(HOST)

rollback: ## Revert the deployed binary to the .prev backup
	@./deploy/rollback.sh $(HOST)

status: ## Show deployed version, schedule and recent run output
	@./deploy/status.sh $(HOST)

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "Usage: make \033[36m<target>\033[0m\n\nTargets:\n"} /^[a-zA-Z0-9_-]+:.*##/ {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)
