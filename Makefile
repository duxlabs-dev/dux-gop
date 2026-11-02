# Makefile for dgop

# Project settings
BINARY_NAME=dgop
SOURCE_DIR=cmd/dgop
BUILD_DIR=bin
INSTALL_DIR=/usr/local/bin

# Go settings
GO=go
GOFLAGS=-ldflags="-s -w"

# Version and build info
VERSION=$(shell git describe --tags --always 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# Build flags with version info
BUILD_LDFLAGS=-ldflags="-s -w -X main.Version=$(VERSION) -X main.buildTime=$(BUILD_TIME) -X main.commit=$(COMMIT)"

.PHONY: all build clean install uninstall test test-integration fmt vet deps help

# Default target
all: build

# Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GO) build $(BUILD_LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(SOURCE_DIR)/*.go
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# Install the binary to system path
install:
	@echo "Installing $(BINARY_NAME) to $(INSTALL_DIR)..."
	@test -f $(BUILD_DIR)/$(BINARY_NAME) || (echo "ERROR: $(BUILD_DIR)/$(BINARY_NAME) not found, run 'make' first" && exit 1)
	@cp $(BUILD_DIR)/$(BINARY_NAME) $(INSTALL_DIR)/$(BINARY_NAME)
	@chmod +x $(INSTALL_DIR)/$(BINARY_NAME)
	@echo "Installation complete"

# Uninstall the binary from system path
uninstall:
	@echo "Uninstalling $(BINARY_NAME) from $(INSTALL_DIR)..."
	@rm -f $(INSTALL_DIR)/$(BINARY_NAME)
	@echo "Uninstall complete"

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR)
	@echo "Clean complete"

# Run tests
test:
	@echo "Running tests..."
	$(GO) test -v ./...

test-integration:
	@echo "Running integration tests against the live host..."
	$(GO) test -v -tags integration ./...

# Format Go code
fmt:
	@echo "Formatting Go code..."
	$(GO) fmt ./...

# Run Go vet
vet:
	@echo "Running go vet..."
	$(GO) vet ./...

# Update dependencies
deps:
	@echo "Updating dependencies..."
	$(GO) mod tidy
	$(GO) mod download

# Development build (with debug info)
dev:
	@echo "Building $(BINARY_NAME) for development..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build -o $(BUILD_DIR)/$(BINARY_NAME) $(SOURCE_DIR)/*.go
	@echo "Development build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# Check Go version compatibility
check-go:
	@echo "Checking Go version..."
	@go version | grep -E "go1\.(2[2-9]|[3-9][0-9])" > /dev/null || (echo "ERROR: Go 1.22 or higher required" && exit 1)
	@echo "Go version OK"

# Build with version info
version: check-go
	@echo "Version: $(VERSION)"
	@echo "Build Time: $(BUILD_TIME)"
	@echo "Commit: $(COMMIT)"

# Help target
help:
	@echo "Available targets:"
	@echo "  all       - Build the binary (default)"
	@echo "  build     - Build the binary"
	@echo "  install   - Install binary to $(INSTALL_DIR)"
	@echo "  uninstall - Remove binary from $(INSTALL_DIR)"
	@echo "  clean     - Clean build artifacts"
	@echo "  test      - Run tests"
	@echo "  fmt       - Format Go code"
	@echo "  vet       - Run go vet"
	@echo "  deps      - Update dependencies"
	@echo "  dev       - Build with debug info"
	@echo "  check-go  - Check Go version compatibility"
	@echo "  version   - Show version information"
	@echo "  help      - Show this help message"
