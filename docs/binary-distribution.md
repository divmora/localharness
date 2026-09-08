# Binary Distribution

The `localharness` binary is a single Go binary cross-compiled for multiple platforms. Every SDK (Go, Python, JS) uses the same universal resolution chain to find it.

## Installation Methods

### Option 1: `go install` (Go users)

```bash
go install github.com/divmora/localharness/cmd/localharness@latest
```

This compiles from source and puts the binary in `$GOBIN` (usually `$GOPATH/bin`).

### Option 2: Download from GitHub Releases

```bash
# Linux amd64
curl -sSL https://github.com/divmora/localharness/releases/latest/download/localharness-0.3.0-linux-amd64.tar.gz | tar xz
sudo mv localharness /usr/local/bin/

# macOS Apple Silicon
curl -sSL https://github.com/divmora/localharness/releases/latest/download/localharness-0.3.0-darwin-arm64.tar.gz | tar xz
sudo mv localharness /usr/local/bin/
```

### Option 3: Zero-install (auto-download)

If the binary isn't found via any other method, the SDK automatically downloads it from GitHub releases on first run and caches it at `~/.divmora/localharness/bin/`.

### Option 4: Python SDK

```bash
pip install localharness
```

The Python SDK includes a `BinaryManager` that handles auto-download.

## Universal Resolution Chain

All SDKs resolve the binary using the same 5-step chain:

```
1. Explicit path      → config.BinaryPath / binary_path param
2. Environment var    → $LOCALHARNESS_BIN
3. System PATH        → `which localharness` (covers go install, manual install)
4. Version cache      → ~/.divmora/localharness/bin/v<VERSION>/localharness
5. Auto-download      → GitHub releases → cache
```

First match wins. Steps 4-5 are fallbacks.

## Shared Cache

All SDKs share the same cache directory:

```
~/.divmora/localharness/bin/
├── v0.3.0/
│   └── localharness           # Cached binary for v0.3.0
├── v0.3.1/
│   └── localharness           # Cached binary for v0.3.1
```

If the Python SDK downloads the binary, the Go ADK finds it instantly — no re-download.

## Environment Variables

| Variable | Description |
|:---|:---|
| `LOCALHARNESS_BIN` | Explicit path to the binary (highest priority after config) |
| `GITHUB_TOKEN` | GitHub API token for higher rate limits during auto-download |

## Version Pinning

Each SDK release is built against a specific `localharness` binary version. The SDK embeds this version string internally. When auto-downloading, it fetches **this exact version** — not "latest" — ensuring consistency.

Development builds (`0.0.0-dev`) skip auto-download and require manual installation.

## Supported Platforms

| OS | Architecture | Asset Suffix |
|:---|:---|:---|
| Linux | x86_64 | `linux-amd64` |
| Linux | ARM64 | `linux-arm64` |
| macOS | Intel | `darwin-amd64` |
| macOS | Apple Silicon | `darwin-arm64` |

## Release Asset Format

Each GitHub release contains:

```
localharness-0.3.0-linux-amd64.tar.gz       # Tarball (binary at root)
localharness-0.3.0-linux-amd64.tar.gz.sha256 # Per-platform checksum
localharness-0.3.0-linux-arm64.tar.gz
localharness-0.3.0-linux-arm64.tar.gz.sha256
localharness-0.3.0-darwin-amd64.tar.gz
localharness-0.3.0-darwin-amd64.tar.gz.sha256
localharness-0.3.0-darwin-arm64.tar.gz
localharness-0.3.0-darwin-arm64.tar.gz.sha256
checksums.txt                                 # All checksums in one file
```

The SDK's `BinaryResolver` downloads the tarball, verifies the SHA256 checksum from `checksums.txt`, extracts the binary, and caches it.

## Building from Source

```bash
# Build for current platform
make build

# Cross-compile all platforms
make cross-compile

# Output in dist/:
# dist/localharness-<VERSION>-linux-amd64.tar.gz
# dist/localharness-<VERSION>-linux-arm64.tar.gz
# dist/localharness-<VERSION>-darwin-amd64.tar.gz
# dist/localharness-<VERSION>-darwin-arm64.tar.gz
# dist/checksums.txt
```

## SDK Implementation Guide

When building a new SDK for LocalHarness, implement the resolution chain in this order:

1. **Check explicit config** — user-provided binary path
2. **Check `$LOCALHARNESS_BIN`** — environment variable override
3. **Check system PATH** — `which localharness`
4. **Check version cache** — `~/.divmora/localharness/bin/v<VERSION>/localharness`
5. **Auto-download** — fetch from `https://api.github.com/repos/divmora/localharness/releases/tags/v<VERSION>`

Reference implementations:
- **Go ADK**: [`sdk/connection/binary_resolver.go`](../sdk/connection/binary_resolver.go)
- **Python SDK**: [`binary_manager.py`](https://github.com/divmora/localharness-sdk-python/blob/main/src/localharness/binary_manager.py)
