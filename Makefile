.PHONY: build test clean install run fmt vet lint build-linux-amd64 build-darwin-arm64 build-all-platforms

# Build variables
BINARY_NAME=logwatch-analyzer
BUILD_DIR=bin
INSTALL_DIR=/opt/logwatch-ai
GO=go
GOFLAGS=-v

# Version info from git
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

# Linker flags for version injection
LDFLAGS_VERSION=-X main.version=$(VERSION) -X main.gitCommit=$(GIT_COMMIT) -X main.buildTime=$(BUILD_TIME)

# Build the application
build:
	@echo "Building $(BINARY_NAME) $(VERSION)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -ldflags="$(LDFLAGS_VERSION)" -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/analyzer

# Build with optimizations (smaller binary)
build-prod:
	@echo "Building $(BINARY_NAME) $(VERSION) for production..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build -ldflags="-s -w $(LDFLAGS_VERSION)" -trimpath -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/analyzer

# Build for Linux AMD64 (Debian 12/Ubuntu 24)
build-linux-amd64:
	@echo "Building $(BINARY_NAME) $(VERSION) for Linux AMD64..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 $(GO) build -ldflags="-s -w $(LDFLAGS_VERSION)" -trimpath -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/analyzer

# Build for macOS ARM64 (Apple Silicon)
build-darwin-arm64:
	@echo "Building $(BINARY_NAME) $(VERSION) for macOS ARM64..."
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=arm64 $(GO) build -ldflags="-s -w $(LDFLAGS_VERSION)" -trimpath -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/analyzer

# Build for all platforms
build-all-platforms: build-linux-amd64 build-darwin-arm64
	@echo "All platform builds complete!"
	@ls -lh $(BUILD_DIR)/$(BINARY_NAME)-*

# Run tests
test:
	@echo "Running tests..."
	$(GO) test -v ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	$(GO) test -v -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html

# Format code
fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...

# Run go vet
vet:
	@echo "Running go vet..."
	$(GO) vet ./...

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@rm -f coverage.out coverage.html

# Install to system directory
install: build-prod
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

# Run the application
run: build
	@$(BUILD_DIR)/$(BINARY_NAME)

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GO) mod download
	$(GO) mod tidy

# Display help
help:
	@echo "Available targets:"
	@echo "  build              - Build the application"
	@echo "  build-prod         - Build optimized production binary"
	@echo "  build-linux-amd64  - Build for Linux AMD64 (Debian 12/Ubuntu 24)"
	@echo "  build-darwin-arm64 - Build for macOS ARM64 (Apple Silicon)"
	@echo "  build-all-platforms- Build for all platforms"
	@echo "  test               - Run tests"
	@echo "  test-coverage      - Run tests with coverage report"
	@echo "  fmt                - Format code"
	@echo "  vet                - Run go vet"
	@echo "  clean              - Remove build artifacts"
	@echo "  install            - Install to $(INSTALL_DIR)"
	@echo "  run                - Build and run the application"
	@echo "  deps               - Download and tidy dependencies"
	@echo "  help               - Show this help message"
