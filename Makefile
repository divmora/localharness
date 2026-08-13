.PHONY: proto build test clean lint deps test-client build-lhctl all cross-compile

# Binary name
BINARY := localharness
BIN_DIR := bin
DIST_DIR := dist
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.0.0-dev")

ifeq ($(OS),Windows_NT)
    EXE_EXT := .exe
else
    EXE_EXT :=
endif

# Proto generation using buf
proto:
	@echo "==> Generating protobuf code..."
	buf generate
	@echo "==> Done."

# Build the binary
build:
	@echo "==> Building $(BINARY)..."
	@mkdir -p $(BIN_DIR)
	go build -ldflags="-X github.com/divmora/localharness/internal/config.HarnessVersion=$(VERSION)" -o $(BIN_DIR)/$(BINARY)$(EXE_EXT) ./cmd/localharness
	@echo "==> Built $(BIN_DIR)/$(BINARY)$(EXE_EXT) ($(VERSION))"

# Run tests
test:
	go test ./... -v

# Lint proto files
lint:
	buf lint

# Clean build artifacts
clean:
	rm -rf $(BIN_DIR) $(DIST_DIR) gen

# Install dependencies
deps:
	go mod tidy

# Build test client
test-client:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/testclient ./cmd/testclient
	@echo "==> Built $(BIN_DIR)/testclient"

# Build lhctl CLI debugger
build-lhctl:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/lhctl ./cmd/lhctl
	@echo "==> Built $(BIN_DIR)/lhctl"

# Full build: proto + binary + tools + agents
all: proto build test-client build-lhctl
	@echo "==> Full build complete."

# Cross-compile for all supported platforms
# Produces: dist/localharness-<VERSION>-<PLATFORM>.tar.gz + checksums.txt
cross-compile:
	@echo "==> Cross-compiling $(BINARY) v$(VERSION) for all platforms..."
	@mkdir -p $(DIST_DIR)
	@for platform in linux-amd64 linux-arm64 darwin-amd64 darwin-arm64; do \
		GOOS=$$(echo $$platform | cut -d- -f1) \
		GOARCH=$$(echo $$platform | cut -d- -f2) \
		go build \
			-ldflags="-s -w -X github.com/divmora/localharness/internal/config.HarnessVersion=$(VERSION)" \
			-o $(DIST_DIR)/$(BINARY) \
			./cmd/localharness && \
		tar -czf $(DIST_DIR)/$(BINARY)-$(VERSION)-$$platform.tar.gz \
			-C $(DIST_DIR) $(BINARY) && \
		rm $(DIST_DIR)/$(BINARY) && \
		echo "    ✓ $$platform"; \
	done
	@echo "==> Generating checksums..."
	@cd $(DIST_DIR) && sha256sum *.tar.gz > checksums.txt
	@echo "==> Done. Artifacts in $(DIST_DIR)/"
	@ls -lh $(DIST_DIR)/
