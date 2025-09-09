# Makefile for kubectl-must-gather

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=gofmt
BINARY_NAME=aks-must-gather
MAIN_PATH=./cmd/aks-must-gather

# Build directory
BUILD_DIR=./bin

.PHONY: all build clean test test-verbose test-race test-cover deps fmt vet lint help

# Default target
all: clean fmt vet test build

# Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME) -v $(MAIN_PATH)

# Clean build artifacts
clean:
	@echo "Cleaning..."
	$(GOCLEAN)
	@rm -rf $(BUILD_DIR)

# Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -v ./...

# Run tests with verbose output
test-verbose:
	@echo "Running tests with verbose output..."
	$(GOTEST) -v -count=1 ./...

# Run tests with race detection
test-race:
	@echo "Running tests with race detection..."
	$(GOTEST) -race -v ./...

# Run tests with coverage
test-cover:
	@echo "Running tests with coverage..."
	$(GOTEST) -cover -v ./...
	@echo "Generating coverage report..."
	$(GOTEST) -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy

# Format code
fmt:
	@echo "Formatting code..."
	$(GOFMT) -s -w .

# Run go vet
vet:
	@echo "Running go vet..."
	$(GOCMD) vet ./...

# Run golint (if available)
lint:
	@echo "Running golint..."
	@command -v golint >/dev/null 2>&1 || { echo "golint not found, skipping..."; exit 0; }
	@golint ./...

# Install the binary
install: build
	@echo "Installing $(BINARY_NAME)..."
	@cp $(BUILD_DIR)/$(BINARY_NAME) $(GOPATH)/bin/

# Run the application
run:
	@echo "Running $(BINARY_NAME)..."
	$(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@$(BUILD_DIR)/$(BINARY_NAME)

# Development build (with debug info)
build-dev:
	@echo "Building $(BINARY_NAME) for development..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) -gcflags="-N -l" -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)

# Cross-compile for different platforms
build-linux:
	@echo "Building for Linux..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 $(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME)-linux $(MAIN_PATH)

build-windows:
	@echo "Building for Windows..."
	@mkdir -p $(BUILD_DIR)
	GOOS=windows GOARCH=amd64 $(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME)-windows.exe $(MAIN_PATH)

build-darwin:
	@echo "Building for macOS..."
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=amd64 $(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin $(MAIN_PATH)

# Build for all platforms
build-all: build-linux build-windows build-darwin

# Help target
help:
	@echo "Available targets:"
	@echo "  all          - Run clean, fmt, vet, test, and build"
	@echo "  build        - Build the binary"
	@echo "  clean        - Clean build artifacts"
	@echo "  test         - Run tests"
	@echo "  test-verbose - Run tests with verbose output"
	@echo "  test-race    - Run tests with race detection"
	@echo "  test-cover   - Run tests with coverage report"
	@echo "  deps         - Download and tidy dependencies"
	@echo "  fmt          - Format code"
	@echo "  vet          - Run go vet"
	@echo "  lint         - Run golint (if available)"
	@echo "  install      - Install the binary to GOPATH/bin"
	@echo "  run          - Build and run the application"
	@echo "  build-dev    - Build with debug info"
	@echo "  build-linux  - Cross-compile for Linux"
	@echo "  build-windows- Cross-compile for Windows"
	@echo "  build-darwin - Cross-compile for macOS"
	@echo "  build-all    - Build for all platforms"
	@echo "  help         - Show this help message"