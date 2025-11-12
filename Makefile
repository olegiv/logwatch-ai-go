.PHONY: build test clean install run fmt vet lint

# Build variables
BINARY_NAME=logwatch-analyzer
BUILD_DIR=bin
INSTALL_DIR=/opt/logwatch-ai
GO=go
GOFLAGS=-v

# Build the application
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/analyzer

# Build with optimizations (smaller binary)
build-prod:
	@echo "Building $(BINARY_NAME) for production..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build -ldflags="-s -w" -trimpath -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/analyzer

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
	@echo "  build         - Build the application"
	@echo "  build-prod    - Build optimized production binary"
	@echo "  test          - Run tests"
	@echo "  test-coverage - Run tests with coverage report"
	@echo "  fmt           - Format code"
	@echo "  vet           - Run go vet"
	@echo "  clean         - Remove build artifacts"
	@echo "  install       - Install to $(INSTALL_DIR)"
	@echo "  run           - Build and run the application"
	@echo "  deps          - Download and tidy dependencies"
	@echo "  help          - Show this help message"
